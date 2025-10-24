#!/usr/bin/env bash
set -eo pipefail # Exit on error, and exit on command failure in pipelines

# --- Configuration ---
# !! SCRIPT PARAMETERS:
#    $1: Your STACKIT Project ID
#    $2: The Kubernetes version to install (e.g., "1.29.0")

PROJECT_ID="$1"
K8S_VERSION="$2" # Example: "1.29.0"

# --- Script Configuration ---
VM_NAME="kube-single-node"
# Can be overridden by environment variable (e.g. SSH_KEY_NAME="my-key" ./script.sh ...)
SSH_KEY_NAME="${SSH_KEY_NAME:-"kube-automation-key"}"
# Can be overridden by environment variable
NETWORK_NAME="${NETWORK_NAME:-}"
# This script uses a 2-vCPU, 4GB-RAM machine type.
# You can find other types in the STACKIT Portal or documentation.
MACHINE_TYPE="c2i.2"
# This script will look for an Ubuntu 22.04 image
IMAGE_NAME_FILTER="Ubuntu 22.04"
# SSH User for Ubuntu
SSH_USER="ubuntu"

# --- Constants ---
MAX_WAIT_TIME=300  # 5 minutes for operations
WAIT_INTERVAL=10   # seconds between checks
SSH_TIMEOUT=300    # 5 minutes for SSH readiness
SSH_CHECK_INTERVAL=10 # seconds between SSH checks

# --- Helper Functions ---
log() {
  printf "[$(date +'%T')] 🔷 %s\n" "$*" >&2
}

log_success() {
  printf "[$(date +'%T')] ✅ %s\n" "$*" >&2
}

log_warn() {
  printf "[$(date +'%T')] ⚠️  %s\n" "$*" >&2
}

log_error() {
  printf "[$(date +'%T')] ❌ %s\n" "$*" >&2
  exit 1
}

check_deps() {
  log "Checking dependencies..."
  command -v stackit >/dev/null 2>&1 || log_error "STACKIT CLI ('stackit') not found. Please install it."
  command -v jq >/dev/null 2>&1 || log_error "jq not found. Please install it."
  command -v ssh >/dev/null 2>&1 || log_error "ssh not found. Please install it."

  # Validate SSH key pair
  local ssh_pub_key_path="$HOME/.ssh/$SSH_KEY_NAME.pub"
  local ssh_priv_key_path="$HOME/.ssh/$SSH_KEY_NAME"

  [[ -f "$ssh_pub_key_path" ]] || log_error "Public SSH key not found at $ssh_pub_key_path. Please generate one with 'ssh-keygen -f $HOME/.ssh/$SSH_KEY_NAME'."
  [[ -f "$ssh_priv_key_path" ]] || log_error "Private SSH key not found at $ssh_priv_key_path."

  # Check key permissions
  [[ $(stat -c %a "$ssh_priv_key_path") == "600" ]] || log_warn "Private key permissions should be 600. Current: $(stat -c %a "$ssh_priv_key_path")"

  # Validate parameters
  [[ -n "$PROJECT_ID" ]] || log_error "Usage: $0 <PROJECT_ID> <K8S_VERSION>"
  [[ -n "$K8S_VERSION" ]] || log_error "Usage: $0 <PROJECT_ID> <K8S_VERSION>"

  # Validate Kubernetes version format (must be like 1.31.13)
  if ! [[ "$K8S_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    log_error "Invalid Kubernetes version format. Must be in format X.Y.Z (e.g., 1.31.13)"
  fi
}

# --- STACKIT API Functions ---

# Creates an SSH key in STACKIT or confirms it exists
# Globals:
#   PROJECT_ID
#   SSH_KEY_NAME
# Arguments:
#   None
# Outputs:
#   Creates SSH key in STACKIT if it doesn't exist
setup_ssh_key() {
  log "Checking for SSH key '$SSH_KEY_NAME'..."

  if ! stackit key-pair describe "$SSH_KEY_NAME" --project-id "$PROJECT_ID" &>/dev/null; then
    log "No existing key found. Creating..."
    # The '@' prefix tells the CLI to read the content from the file
    if ! stackit key-pair create "$SSH_KEY_NAME" -y \
      --project-id "$PROJECT_ID" \
      --public-key "@$HOME/.ssh/$SSH_KEY_NAME.pub"; then
      log_error "Failed to create SSH key '$SSH_KEY_NAME' in STACKIT."
    fi
    log_success "SSH key '$SSH_KEY_NAME' created."
  else
    log_success "SSH key '$SSH_KEY_NAME' already exists."
  fi
}

# Finds a network ID by name, using a default name if NETWORK_NAME is not set.
# Creates the network if it doesn't exist (only applicable for the default name).
find_network_id() {
  log "Finding a network..."
  local network_id
  local default_net_name="kube-net-default" # Stable default name
  # Use NETWORK_NAME if set, otherwise fall back to default_net_name
  local target_network_name="${NETWORK_NAME:-$default_net_name}"

  log "Target network name: '$target_network_name'"

  # Check if the target network exists
  network_id=$(stackit network list --project-id "$PROJECT_ID" --output-format json | \
    jq -r --arg name "$target_network_name" 'map(select(.name == $name)) | .[0].networkId')

  if [[ -z "$network_id" || "$network_id" == "null" ]]; then
    # Network does not exist
    if [[ -n "$NETWORK_NAME" && "$target_network_name" == "$NETWORK_NAME" ]]; then
      # If a specific NETWORK_NAME was provided and not found, error out.
      log_error "Specified network '$target_network_name' not found in project."
    else
      # If no NETWORK_NAME was provided (or it was empty), and the default network wasn't found, create it.
      log "Network '$target_network_name' not found. Creating it..."
      network_id=$(stackit network create --name "$target_network_name" \
        --project-id "$PROJECT_ID" \
        --output-format json -y | jq -r ".id") # .id is the correct field for the create output

      [[ -n "$network_id" && "$network_id" != "null" ]] || log_error "Failed to create new network '$target_network_name'."
      log_success "Created network '$target_network_name' with ID: $network_id"
    fi
  else
    # Network was found
    log_success "Found network '$target_network_name' with ID: $network_id"
  fi

  echo "$network_id"
}

# Finds the latest Ubuntu image
find_image_id() {
  log "Finding latest '$IMAGE_NAME_FILTER' image..."
  local image_id
  image_id=$(stackit image list --project-id "$PROJECT_ID" --output-format json | \
    jq -r --arg f "$IMAGE_NAME_FILTER" \
    '[.[] | select(.name | test("^" + $f + "$"; "i"))] | sort_by(.version) | .[-1].id')

  [[ -n "$image_id" && "$image_id" != "null" ]] || log_error "No image found matching '$IMAGE_NAME_FILTER'."
  log_success "Found image ID: $image_id"
  echo "$image_id"
}

# --- Kubeadm Setup Function ---

# This function defines the setup script that will be run on the remote VM
get_kubeadm_script() {
cat << EOF
#!/bin/bash
set -eo pipefail # Exit on error

log() {
  # Use \$* (escaped) to print all remote arguments as a single string
  printf "[KUBE] --- %s\n" "\$*"
}

log "RUNNING REMOTE KUBEADM SETUP"
log "Starting Kubernetes single-node setup..."
export K8S_VERSION="$K8S_VERSION"

# 1. Disable Swap
log "Disabling swap..."
sudo swapoff -a
# And comment out swap in fstab
sudo sed -i -e '/ swap /s/^#*\(.*\)$/#\1/g' /etc/fstab

# 2. Set up kernel modules and sysctl
log "Configuring kernel modules and sysctl..."
cat <<EOT | sudo tee /etc/modules-load.d/k8s.conf
overlay
br_netfilter
EOT
sudo modprobe overlay
sudo modprobe br_netfilter

cat <<EOT | sudo tee /etc/sysctl.d/k8s.conf
net.bridge.bridge-nf-call-ip6tables = 1
net.bridge.bridge-nf-call-iptables = 1
net.ipv4.ip_forward = 1
EOT
sudo sysctl --system

# 3. Install containerd
log "Installing containerd..."
sudo apt-get update
sudo apt-get install -y containerd
sudo mkdir -p /etc/containerd
sudo containerd config default | sudo tee /etc/containerd/config.toml
# Set CgroupDriver to systemd
sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
sudo systemctl restart containerd

# 4. Install kubeadm, kubelet, kubectl
log "Installing Kubernetes components (v$K8S_VERSION)..."
sudo apt-get update
sudo apt-get install -y apt-transport-https ca-certificates curl gpg jq

# Create a stable path for the key
K8S_APT_KEYRING="/etc/apt/keyrings/kubernetes-apt-keyring.gpg"
# Extract major and minor version for repository URL (e.g., 1.29 from 1.29.0)
K8S_MAJOR_MINOR="${K8S_VERSION%.*}"
K8S_KEY_URL="https://pkgs.k8s.io/core:/stable:/v\${K8S_MAJOR_MINOR}/deb/Release.key"
K8S_REPO_URL="https://pkgs.k8s.io/core:/stable:/v\${K8S_MAJOR_MINOR}/deb/"
K8S_TEMP_KEY_PATH="/tmp/k8s-release.key"

log "Downloading K8s signing key from \${K8S_KEY_URL}..."
# Download to a temp file first.
curl -fL "\${K8S_KEY_URL}" -o "\${K8S_TEMP_KEY_PATH}"

log "Dearmoring key and adding to \${K8S_APT_KEYRING}..."
sudo gpg --dearmor --batch --yes --output "\${K8S_APT_KEYRING}" "\${K8S_TEMP_KEY_PATH}"

# Clean up temp file
rm "\${K8S_TEMP_KEY_PATH}"

log "Adding K8s apt repository..."
echo "deb [signed-by=\${K8S_APT_KEYRING}] \${K8S_REPO_URL} /" | sudo tee /etc/apt/sources.list.d/kubernetes.list
# --- END MODIFIED SECTION ---

sudo apt-get update
# Pin the version
sudo apt-get install -y kubelet="${K8S_VERSION}-*" kubeadm="${K8S_VERSION}-*" kubectl="${K8S_VERSION}-*"
sudo apt-mark hold kubelet kubeadm kubectl

# 5. Initialize the cluster (IDEMPOTENCY CHECK)
if [ ! -f /etc/kubernetes/admin.conf ]; then
  log "Initializing cluster with kubeadm..."
  # Note: Using Calico's default CIDR
  sudo kubeadm init --pod-network-cidr=192.168.0.0/16 --kubernetes-version="$K8S_VERSION"

  # 6. Configure kubectl for the ubuntu user
  # Use \$USER to get the remote user (e.g., 'ubuntu')
  log "Configuring kubectl for \$USER user..."
  mkdir -p \$HOME/.kube
  sudo cp -i /etc/kubernetes/admin.conf \$HOME/.kube/config
  sudo chown \$(id -u):\$(id -g) \$HOME/.kube/config
else
  log "Cluster already initialized (admin.conf exists). Skipping init and user config."
fi

# 7. Install Calico CNI (IDEMPOTENCY CHECK)
# Check if operator is already there
if ! kubectl get deployment -n tigera-operator tigera-operator &>/dev/null; then
  log "Installing Calico CNI..."
  # Using Calico v3.28.0. You may want to update this URL in the future.
  kubectl create -f https://raw.githubusercontent.com/projectcalico/calico/v3.28.0/manifests/tigera-operator.yaml
  kubectl create -f https://raw.githubusercontent.com/projectcalico/calico/v3.28.0/manifests/custom-resources.yaml
else
  log "Calico operator (tigera-operator) already exists. Skipping CNI installation."
fi

# 8. Untaint the node to allow pods to run on the control-plane (IDEMPOTENCY CHECK)
# Wait for node to be ready before untainting (after CNI is installed)
log "Waiting for node to be ready after CNI installation..."
kubectl wait --for=condition=Ready node --all --timeout=60s

# Check if the node has the control-plane taint before trying to remove it
if kubectl get nodes -o json | jq -e '.items[0].spec.taints[] | select(.key == "node-role.kubernetes.io/control-plane")' >/dev/null 2>&1; then
  log "Untainting control-plane node..."
  kubectl taint nodes --all node-role.kubernetes.io/control-plane-
fi

log "✅ Kubernetes single-node cluster setup script finished."
log "Wait a minute for pods to come up, then check with 'kubectl get nodes -o wide' and 'kubectl get pods -A'."
EOF
}

# --- Main Execution ---

main() {
  check_deps

  log "Starting STACKIT VM & Kubeadm setup..."
  log "Project: $PROJECT_ID, VM: $VM_NAME, K8s: $K8S_VERSION"

  # 1. Prepare prerequisites in STACKIT
  setup_ssh_key
  local network_id
  network_id=$(find_network_id)
  local image_id
  image_id=$(find_image_id)

  # 2. Check if server already exists
  log "Checking if server '$VM_NAME' already exists..."
  local server_id=""
  server_id=$(stackit server list --project-id "$PROJECT_ID" --output-format json | \
    jq -r --arg name "$VM_NAME" '.[] | select(.name == $name) | .id')

  if [[ -n "$server_id" && "$server_id" != "null" ]]; then
    log_success "Server '$VM_NAME' already exists with ID: $server_id. Using existing server."
  else
    # Server does not exist, create it
    log "Server '$VM_NAME' not found. Sending 'stackit server create' command..."
    local creation_output_json
    creation_output_json=$(stackit server create -y --name "$VM_NAME" \
      --project-id "$PROJECT_ID" \
      --machine-type "$MACHINE_TYPE" \
      --network-id "$network_id" \
      --keypair-name "$SSH_KEY_NAME" \
      --boot-volume-source-id "$image_id" \
      --boot-volume-source-type image \
      --boot-volume-size "100" \
      --output-format json)

    local create_exit_code=$?
    if [[ $create_exit_code -ne 0 ]]; then
        log_error "Failed to execute 'stackit server create' for '$VM_NAME'. Exit code: $create_exit_code. Output: $creation_output_json"
    fi

    server_id=$(echo "$creation_output_json" | jq -r '.id')
    if [[ -z "$server_id" || "$server_id" == "null" ]]; then
      log_error "Failed to extract server ID from creation output: $creation_output_json"
    fi
    log_success "Create command accepted. VM '$VM_NAME' is provisioning with ID: $server_id."
  fi

  # 3. Check for Public IP and attach if missing
  local current_server_details
  current_server_details=$(stackit server describe "$server_id" --project-id "$PROJECT_ID" --output-format json)
  local public_ip
  local existing_ip
  existing_ip=$(echo "$current_server_details" | jq -r '.nics[] | select(.publicIp != null) | .publicIp' | head -n 1)

  if [[ -n "$existing_ip" && "$existing_ip" != "null" ]]; then
    public_ip="$existing_ip"
    log "Using existing Public IP $public_ip from server '$VM_NAME'."
  else
    # No public IP found, create a new one
    log "Creating a Public IP..."
    local public_ip_json
    public_ip_json=$(stackit public-ip create -y --project-id "$PROJECT_ID" --output-format json)

    local pip_create_exit_code=$?
    if [[ $pip_create_exit_code -ne 0 ]]; then
        log_error "Failed to execute 'stackit public-ip create'. Exit code: $pip_create_exit_code. Output: $public_ip_json"
    fi
    public_ip=$(echo "$public_ip_json" | jq -r '.ip')
    if [[ -z "$public_ip" || "$public_ip" == "null" ]]; then
        log_error "Failed to extract IP from public IP creation output: $public_ip_json"
    fi
    log_success "Created Public IP: $public_ip"
  fi

  # Check if the public IP is already attached to the server
  local attached_ip
  attached_ip=$(echo "$current_server_details" | jq -r --arg target_ip "$public_ip" '.nics[] | select(.publicIp != null and .publicIp == $target_ip) | .publicIp' | head -n 1)

  if [[ "$attached_ip" == "$public_ip" ]]; then
    log "Public IP $public_ip already attached to server '$VM_NAME'."
  elif [[ -n "$attached_ip" && "$attached_ip" != "null" ]]; then
    # A *different* IP is attached. This is unexpected. Error out.
    log_error "Server '$VM_NAME' already has a different Public IP attached $attached_ip. Cannot attach $public_ip."
  else
    # No IP or expected IP not attached, proceed with attach
    log "Attaching Public IP $public_ip to server $server_id..."

    # We need the ID of the public IP to attach it
    local public_ip_id
    public_ip_id=$(stackit public-ip list --project-id "$PROJECT_ID" --output-format json | \
      jq -r --arg ip "$public_ip" 'map(select(.ip == $ip)) | .[0].id')
      
    if [[ -z "$public_ip_id" || "$public_ip_id" == "null" ]]; then
        log_error "Could not find Public IP ID for IP $public_ip."
    fi

    stackit server public-ip attach "$public_ip_id" --server-id "$server_id" --project-id "$PROJECT_ID" -y
    local attach_exit_code=$?
    if [[ $attach_exit_code -ne 0 ]]; then
        log_error "Failed to attach Public IP $public_ip_id to server $server_id. Exit code: $attach_exit_code."
    fi
    log_success "Public IP attach command sent."
  fi

  # 4. Wait for the server to be "ACTIVE" and get its IP address value
  local vm_status="" # Reset status before loop
  local ip_attached=""
  local security_group_id=""
  log "Waiting for VM '$VM_NAME' (ID: $server_id) to become 'ACTIVE' and IP to appear..."

  # Loop until status is ACTIVE AND the target IP is reported in the NICs
  local elapsed_time=0
  while [[ "$vm_status" != "ACTIVE" || "$ip_attached" == "null" || -z "$ip_attached" ]]; do
    sleep $WAIT_INTERVAL
    elapsed_time=$((elapsed_time + WAIT_INTERVAL))
    echo -n "." >&2 # Progress to stderr

    if [[ $elapsed_time -ge $MAX_WAIT_TIME ]]; then
      log_error "Timeout waiting for VM to become active (max $MAX_WAIT_TIME seconds)"
    fi

    # Re-fetch details in the loop
    local vm_details
    vm_details=$(stackit server describe "$server_id" --project-id "$PROJECT_ID" --output-format json)

    vm_status=$(echo "$vm_details" | jq -r '.status')
    ip_attached=$(echo "$vm_details" | jq -r --arg target_ip "$public_ip" '.nics[] | select(.publicIp != null and .publicIp == $target_ip) | .publicIp' | head -n 1)

    # Also grab the security group ID while we're at it
    if [[ -z "$security_group_id" || "$security_group_id" == "null" ]]; then
      security_group_id=$(echo "$vm_details" | jq -r '.nics[] | .securityGroups[]' | head -n 1)
    fi

    # Add a check for failure states
    if [[ "$vm_status" == "ERROR" || "$vm_status" == "FAILED" ]]; then
        log_error "VM '$VM_NAME' entered status '$vm_status'. Aborting."
    fi
  done
  echo >&2 # Newline after progress dots

  log_success "VM is ACTIVE! Public IP Address: $public_ip"

  # 5. Setup security group for SSH
  if [[ -z "$security_group_id" || "$security_group_id" == "null" ]]; then
    log_error "Could not determine security group ID for the VM's NIC."
  fi
  
  local security_group_name
  security_group_name=$(stackit security-group describe "$security_group_id" --project-id "$PROJECT_ID" --output-format json | jq -r '.name')
    
  # Check if SSH rule exists in the security group
  local ssh_rule_exists
  ssh_rule_exists=$(stackit security-group rule list --security-group-id "$security_group_id" \
    --project-id "$PROJECT_ID" --output-format json | \
    jq -r 'map(select(.portRange.min == 22 and .portRange.max == 22 and .protocol.name == "tcp" and .direction == "ingress")) | length')

  if [[ "$ssh_rule_exists" -eq 0 ]]; then
    log "Adding SSH rule to security group '$security_group_name'..."
    stackit security-group rule create --security-group-id "$security_group_id" \
      --direction ingress --protocol-name tcp --port-range-max 22 --port-range-min 22 \
      --description "SSH Access" --project-id "$PROJECT_ID" -y
    log_success "Added SSH rule to security group '$security_group_name'"
  else
    log_success "SSH rule already exists in security group '$security_group_name'"
  fi

  # 6. Wait for SSH to be ready
  log "Waiting for SSH server to be ready on $public_ip..."
  local ssh_ready=false
  local elapsed_time=0

  while [[ $elapsed_time -lt $SSH_TIMEOUT ]]; do
    if ssh -o "StrictHostKeyChecking=no" -o "ConnectTimeout=5" -o "IdentitiesOnly=yes" -i "$HOME/.ssh/$SSH_KEY_NAME" "$SSH_USER@$public_ip" "echo 'SSH is up'" &>/dev/null; then
      ssh_ready=true
      break
    fi
    echo -n "." >&2
    sleep $SSH_CHECK_INTERVAL
    elapsed_time=$((elapsed_time + SSH_CHECK_INTERVAL))
  done
  echo >&2

  if [[ "$ssh_ready" != "true" ]]; then
    log_error "SSH connection timed out after $SSH_TIMEOUT seconds. Please check:
1. Security group rules in the STACKIT Portal
2. The VM is running and accessible
3. Your SSH key is correctly configured"
  fi
  log_success "SSH is ready."

   # 7. Copy and execute the Kubeadm setup script
   log "Copying and executing Kubeadm setup script on the VM..."
   local setup_script
   setup_script=$(get_kubeadm_script)

   # Pass the script content as a command to SSH
   ssh -o "StrictHostKeyChecking=no" -o "IdentitiesOnly=yes" -i "$HOME/.ssh/$SSH_KEY_NAME" "$SSH_USER@$public_ip" "$setup_script"

   log_success "All done!"
   log "You can now access your cluster:"
   echo >&2
   echo "  ssh -o StrictHostKeyChecking=no -o IdentitiesOnly=yes -i $HOME/.ssh/$SSH_KEY_NAME $SSH_USER@$public_ip" >&2
   echo "  (Once inside: kubectl get nodes)" >&2
   echo >&2
   echo "To get the kubeconfig for local use:" >&2
   echo "  ssh -o StrictHostKeyChecking=no -o IdentitiesOnly=yes -i $HOME/.ssh/$SSH_KEY_NAME $SSH_USER@$public_ip 'cat .kube/config' > ./$VM_NAME.kubeconfig" >&2
   echo "  KUBECONFIG=./$VM_NAME.kubeconfig kubectl get nodes" >&2
}

main "$@"
