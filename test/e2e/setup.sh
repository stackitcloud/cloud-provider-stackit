#!/usr/bin/env bash
set -eo pipefail # Exit on error, and exit on command failure in pipelines

# --- Configuration ---
# !! SCRIPT PARAMETERS:
#    $1: Action (create|destroy)
#    $2: Your STACKIT Project ID
#    $3: The Kubernetes version to install (e.g., "1.29.0")

ACTION="$1"
PROJECT_ID="$2"
K8S_VERSION="$3" # Example: "1.29.0"

# --- Script Configuration ---
VM_NAME="stackit-ccm-test"
# Can be overridden by environment variable (e.g. SSH_KEY_NAME="my-key" ./script.sh ...)
SSH_KEY_NAME="${SSH_KEY_NAME:-$VM_NAME}"
# Can be overridden by environment variable
NETWORK_NAME="${NETWORK_NAME:-$VM_NAME}"
# This script uses a 4-vCPU, 8GB-RAM machine type.
MACHINE_TYPE="c2i.4"
# This script will look for an Ubuntu 22.04 image
IMAGE_NAME_FILTER="Ubuntu 22.04"
# SSH User for Ubuntu
SSH_USER="ubuntu"

# Inventory file to track created resources
INVENTORY_FILE="test/e2e/inventory-$PROJECT_ID-$VM_NAME.json"
# Path to store the Service Account key
SA_KEY_PATH="test/e2e/sa-key-$PROJECT_ID-$VM_NAME.json"
# Path to store the kubeadm cluster kubeconfig
KUBECONFIG_PATH="test/e2e/kubeconfig-$PROJECT_ID-$VM_NAME.yaml"

# --- Constants ---
MAX_WAIT_TIME=300  # 5 minutes for operations
WAIT_INTERVAL=10   # seconds between checks
SSH_TIMEOUT=300    # 5 minutes for SSH readiness
SSH_CHECK_INTERVAL=10 # seconds between SSH checks

DEPLOY_REPO_URL=https://github.com/stackitcloud/cloud-provider-stackit
# Can be overridden by environment variable to force a specific branch
DEPLOY_BRANCH="${DEPLOY_BRANCH:-}"

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

check_auth() {
  log "Checking STACKIT authentication..."
  if stackit project list < /dev/null &> /dev/null; then
    log_success "Session is active."
  else
    log_error "Authentication is required. Please run 'stackit auth login' manually."
  fi
}

check_deps() {
  log "Checking dependencies..."
  command -v stackit >/dev/null 2>&1 || log_error "STACKIT CLI ('stackit') not found. Please install it."
  command -v jq >/dev/null 2>&1 || log_error "jq not found. Please install it."
  command -v ssh >/dev/null 2>&1 || log_error "ssh not found. Please install it."
  command -v base64 >/dev/null 2>&1 || log_error "base64 not found. Please install it."

  # Validate SSH key pair
  local ssh_pub_key_path="$HOME/.ssh/$SSH_KEY_NAME.pub"
  local ssh_priv_key_path="$HOME/.ssh/$SSH_KEY_NAME"

  [[ -f "$ssh_pub_key_path" ]] || log_error "Public SSH key not found at $ssh_pub_key_path. Please generate one with 'ssh-keygen -f $HOME/.ssh/$SSH_KEY_NAME'."
  [[ -f "$ssh_priv_key_path" ]] || log_error "Private SSH key not found at $ssh_priv_key_path."

  # Check key permissions
  [[ $(stat -c %a "$ssh_priv_key_path") == "600" ]] || log_warn "Private key permissions should be 600. Current: $(stat -c %a "$ssh_priv_key_path")"

  # Validate parameters
  [[ -n "$PROJECT_ID" ]] || log_error "Usage: $0 <create|destroy> <PROJECT_ID> <K8S_VERSION>"
  [[ -n "$K8S_VERSION" ]] || log_error "Usage: $0 <create|destroy> <PROJECT_ID> <K8S_VERSION>"

  # Validate Kubernetes version format (must be like 1.31.13)
  if ! [[ "$K8S_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    log_error "Invalid Kubernetes version format. Must be in format X.Y.Z (e.g., 1.31.13)"
  fi

  # Validate VM_NAME format (must only contain letters and hyphens)
  if ! [[ "$VM_NAME" =~ ^[a-zA-Z-]+$ ]]; then
    log_error "Invalid VM name format. Must only contain letters and hyphens (e.g., 'my-vm-name')"
  fi
}

# --- Inventory Management ---

# Load inventory from file
load_inventory() {
  if [[ -f "$INVENTORY_FILE" ]]; then
    cat "$INVENTORY_FILE" 2>/dev/null || echo "{}"
  else
    echo "{}"
  fi
}

# Save inventory to file
save_inventory() {
  local inventory_content="$1"
  echo "$inventory_content" > "$INVENTORY_FILE"
}

# Update inventory with a new resource
update_inventory() {
  local resource_type="$1"
  local resource_id="$2"
  local resource_name="$3"

  local inventory
  inventory=$(load_inventory)

  # Use jq to add or update the resource
  local updated_inventory
  updated_inventory=$(echo "$inventory" | jq --arg type "$resource_type" --arg id "$resource_id" --arg name "$resource_name" \
    '.[$type] = {id: $id, name: $name}')

  save_inventory "$updated_inventory"
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
    update_inventory "ssh_key" "$SSH_KEY_NAME" "$SSH_KEY_NAME"
  else
    log_success "SSH key '$SSH_KEY_NAME' already exists."
  fi
}

# Finds a network ID by name, or creates it.
# Globals:
#   PROJECT_ID
#   NETWORK_NAME
# Arguments:
#   None
# Outputs:
#   STDOUT: The network ID
ensure_network() {
  log "Finding network '$NETWORK_NAME'..."
  local network_id

  # Check if the target network exists
  network_id=$(stackit network list --project-id "$PROJECT_ID" --output-format json | \
    jq -r --arg name "$NETWORK_NAME" 'map(select(.name == $name)) | .[0].networkId')

  if [[ -z "$network_id" || "$network_id" == "null" ]]; then
    log "Network '$NETWORK_NAME' not found. Creating it..."
    network_id=$(stackit network create --name "$NETWORK_NAME" \
      --project-id "$PROJECT_ID" \
      --output-format json -y | jq -r ".networkId")

    [[ -n "$network_id" && "$network_id" != "null" ]] || log_error "Failed to create new network '$NETWORK_NAME'."
    log_success "Created network '$NETWORK_NAME' with ID: $network_id"
    update_inventory "network" "$network_id" "$NETWORK_NAME"
  else
    log_success "Found network '$NETWORK_NAME' with ID: $network_id"
  fi

  echo "$network_id"
}

# Finds the latest Ubuntu image
# Globals:
#   PROJECT_ID
#   IMAGE_NAME_FILTER
# Arguments:
#   None
# Outputs:
#   STDOUT: The image ID
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

# Finds or creates a Service Account and Key
# Globals:
#   PROJECT_ID
#   VM_NAME
#   SA_KEY_PATH
#   WAIT_INTERVAL
# Arguments:
#   None
# Outputs:
#   STDOUT: The Base64-encoded SA key
ensure_service_account() {
  log "Setting up Service Account..."
  local sa_name=$VM_NAME
  local sa_json
  sa_json=$(stackit service-account list --project-id "$PROJECT_ID" --output-format json)
  
  local sa_email
  sa_email=$(echo "$sa_json" | jq -r --arg name "$sa_name" '.[] | select(.email | startswith($name)) | .email')

  if [[ -z "$sa_email" || "$sa_email" == "null" ]]; then
    log "Service account not found. Creating '$sa_name'..."
    sa_email=$(stackit service-account create --name "$sa_name" --project-id "$PROJECT_ID" -y --output-format json | jq -r '.email')
    if [[ -z "$sa_email" || "$sa_email" == "null" ]]; then
      log_error "Failed to create service account '$sa_name'."
    fi

    log_success "Created service account '$sa_name' with ID: $sa_email"
    update_inventory "service_account" "$sa_email" "$sa_name"
    sleep $WAIT_INTERVAL # Yes, thats required because the sa is not really ready yet
  else
    log_success "Service account '$sa_name' already exists with ID: $sa_email"
  fi

  # Add roles
  log "Assigning required roles to service account $sa_name..."
  for role in alb.admin blockstorage.admin dns.admin nlb.admin; do
    stackit project member add "$sa_email" --project-id "$PROJECT_ID" --role "$role" -y &>/dev/null
  done
  log_success "Roles assigned."

  # Create key if it doesn't exist locally
  if [[ -f "$SA_KEY_PATH" ]]; then
    log_success "Service account key file already exists: $SA_KEY_PATH"
  else
    log "Creating service account key for $sa_name..."
    if ! stackit service-account key create --email "$sa_email" --project-id "$PROJECT_ID" -y --output-format json | jq . > "$SA_KEY_PATH"; then
      rm -f "$SA_KEY_PATH" # Clean up empty file on failure
      log_error "Failed to create service account key for $sa_email."
    fi

    if [[ ! -s "$SA_KEY_PATH" ]]; then
        log_error "Failed to save service account key to $SA_KEY_PATH (file is empty)."
    fi
    log_success "Service Account key saved to $SA_KEY_PATH"
  fi

  # Read and Base64-encode the key for the K8s secret
  local sa_key_json
  sa_key_json=$(cat "$SA_KEY_PATH")
  [[ -n "$sa_key_json" ]] || log_error "Failed to read SA key from $SA_KEY_PATH"
  
  # Use -w 0 for no line wraps.
  local sa_key_b64
  sa_key_b64=$(echo -n "$sa_key_json" | base64 -w 0)
  [[ -n "$sa_key_b64" ]] || log_error "Failed to base64 encode SA key."
  log "Service account key encoded."
  
  echo "$sa_key_b64"
}

# Finds or creates a Security Group and rules
# Globals:
#   PROJECT_ID
#   VM_NAME
# Arguments:
#   None
# Outputs:
#   STDOUT: The security group ID
ensure_security_group() {
  log "Setting up security group for SSH and K8s API..."
  local security_group_id
  local security_group_name=$VM_NAME

  # Check if security group already exists
  security_group_id=$(stackit security-group list --project-id "$PROJECT_ID" --output-format json | \
    jq -r --arg name "$security_group_name" 'map(select(.name == $name)) | .[0].id')

  if [[ -z "$security_group_id" || "$security_group_id" == "null" ]]; then
    log "Security group '$security_group_name' not found. Creating..."
    security_group_id=$(stackit security-group create --name "$security_group_name" \
      --project-id "$PROJECT_ID" --output-format json -y | jq -r '.id')

    if [[ -z "$security_group_id" || "$security_group_id" == "null" ]]; then
      log_error "Failed to create security group '$security_group_name'."
    fi
    log_success "Created security group '$security_group_name' with ID: $security_group_id"
    update_inventory "security_group" "$security_group_id" "$security_group_name"
  else
    log_success "Security group '$security_group_name' already exists with ID: $security_group_id"
  fi

  # Check if SSH rule exists in the security group
  local ssh_rule_exists
  ssh_rule_exists=$(stackit security-group rule list --security-group-id "$security_group_id" \
    --project-id "$PROJECT_ID" --output-format json | \
    jq -r 'map(select(.portRange.min == 22 and .portRange.max == 22 and .protocol.name == "tcp" and .direction == "ingress")) | length')

  if [[ "$ssh_rule_exists" -eq 0 ]]; then
    log "Adding SSH rule to security group '$security_group_name'..."
    # Add >/dev/null to silence standard output
    stackit security-group rule create --security-group-id "$security_group_id" \
      --direction ingress --protocol-name tcp --port-range-max 22 --port-range-min 22 \
      --description "SSH Access" --project-id "$PROJECT_ID" -y >/dev/null
    log_success "Added SSH rule to security group '$security_group_name'"
  else
    log_success "SSH rule already exists in security group '$security_group_name'"
  fi

  # Check if API rule exists in the security group
  local api_rule_exists
  api_rule_exists=$(stackit security-group rule list --security-group-id "$security_group_id" \
    --project-id "$PROJECT_ID" --output-format json | \
    jq -r 'map(select(.portRange.min == 6443 and .portRange.max == 6443 and .protocol.name == "tcp" and .direction == "ingress")) | length')

  if [[ "$api_rule_exists" -eq 0 ]]; then
    log "Adding API rule to security group '$security_group_name'..."
    # Add >/dev/null to silence standard output
    stackit security-group rule create --security-group-id "$security_group_id" \
      --direction ingress --protocol-name tcp --port-range-max 6443 --port-range-min 6443 \
      --description "API Access" --project-id "$PROJECT_ID" -y >/dev/null
    log_success "Added API rule to security group '$security_group_name'"
  else
    log_success "API rule already exists in security group '$security_group_name'"
  fi
  
  # This is now the *only* thing that will be sent to standard output
  echo "$security_group_id"
}

# Finds or creates a Server instance
# Globals:
#   PROJECT_ID
#   VM_NAME
#   MACHINE_TYPE
#   SSH_KEY_NAME
# Arguments:
#   $1: network_id
#   $2: security_group_id
#   $3: image_id
# Outputs:
#   STDOUT: The server ID
ensure_server_instance() {
  local network_id="$1"
  local security_group_id="$2"
  local image_id="$3"
  
  log "Checking if server '$VM_NAME' already exists..."
  local server_id=""
  server_id=$(stackit server list --project-id "$PROJECT_ID" --output-format json | \
    jq -r --arg name "$VM_NAME" '.[] | select(.name == $name) | .id')

  if [[ -n "$server_id" && "$server_id" != "null" ]]; then
    log_success "Server '$VM_NAME' already exists with ID: $server_id. Using existing server."
  else
    # Server does not exist, create it
    log "Server '$VM_NAME' not found. Creating server..."
    local creation_output_json
    creation_output_json=$(stackit server create -y --name "$VM_NAME" \
      --project-id "$PROJECT_ID" \
      --machine-type "$MACHINE_TYPE" \
      --network-id "$network_id" \
      --keypair-name "$SSH_KEY_NAME" \
      --security-groups "$security_group_id" \
      --boot-volume-delete-on-termination \
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
    update_inventory "server" "$server_id" "$VM_NAME"
  fi
  
  echo "$server_id"
}

# Finds or creates a Public IP and attaches it
# Globals:
#   PROJECT_ID
#   VM_NAME
# Arguments:
#   $1: server_id
# Outputs:
#   STDOUT: The public IP address
ensure_public_ip() {
  local server_id="$1"
  
  log "Setting up Public IP for server '$VM_NAME' (ID: $server_id)..."
  local current_server_details
  current_server_details=$(stackit server describe "$server_id" --project-id "$PROJECT_ID" --output-format json)
  
  local public_ip
  public_ip=$(echo "$current_server_details" | jq -r '.nics[] | select(.publicIp != null) | .publicIp' | head -n 1)

  if [[ -n "$public_ip" && "$public_ip" != "null" ]]; then
    log_success "Server already has Public IP: $public_ip"
    echo "$public_ip"
    return
  fi

  log "No existing IP found on server. Creating a new Public IP..."
  local public_ip_json
  public_ip_json=$(stackit public-ip create -y --project-id "$PROJECT_ID" --output-format json)

  local pip_create_exit_code=$?
  if [[ $pip_create_exit_code -ne 0 ]]; then
      log_error "Failed to execute 'stackit public-ip create'. Exit code: $pip_create_exit_code. Output: $public_ip_json"
  fi
  
  public_ip=$(echo "$public_ip_json" | jq -r '.ip')
  local public_ip_id
  public_ip_id=$(echo "$public_ip_json" | jq -r '.id')
  
  if [[ -z "$public_ip" || "$public_ip" == "null" ]]; then
      log_error "Failed to extract IP from public IP creation output: $public_ip_json"
  fi
  
  log_success "Created Public IP: $public_ip"
  update_inventory "public_ip" "$public_ip_id" "$public_ip"

  log "Attaching Public IP $public_ip to server $server_id..."
  stackit server public-ip attach "$public_ip_id" --server-id "$server_id" --project-id "$PROJECT_ID" -y
  
  local attach_exit_code=$?
  if [[ $attach_exit_code -ne 0 ]]; then
      log_error "Failed to attach Public IP $public_ip_id to server $server_id. Exit code: $attach_exit_code."
  fi
  log_success "Public IP attach command sent."

  echo "$public_ip"
}

# --- Wait and Config Functions ---

# Waits for the VM to be ACTIVE and the IP to be attached
# Globals:
#   PROJECT_ID
#   VM_NAME
#   WAIT_INTERVAL
#   MAX_WAIT_TIME
# Arguments:
#   $1: server_id
#   $2: public_ip
# Outputs:
#   None
wait_for_vm_ready() {
  local server_id="$1"
  local public_ip="$2"
  
  log "Waiting for VM '$VM_NAME' (ID: $server_id) to become 'ACTIVE' and IP $public_ip to appear..."
  local vm_status=""
  local ip_attached=""

  # Loop until status is ACTIVE AND the target IP is reported in the NICs
  local elapsed_time=0
  while [[ "$vm_status" != "ACTIVE" || "$ip_attached" == "null" || -z "$ip_attached" ]]; do
    sleep $WAIT_INTERVAL
    elapsed_time=$((elapsed_time + WAIT_INTERVAL))
    echo -n "." >&2

    if [[ $elapsed_time -ge $MAX_WAIT_TIME ]]; then
      log_error "Timeout waiting for VM to become active (max $MAX_WAIT_TIME seconds)"
    fi

    # Re-fetch details
    local vm_details
    vm_details=$(stackit server describe "$server_id" --project-id "$PROJECT_ID" --output-format json)

    vm_status=$(echo "$vm_details" | jq -r '.status')
    ip_attached=$(echo "$vm_details" | jq -r --arg target_ip "$public_ip" '.nics[] | select(.publicIp != null and .publicIp == $target_ip) | .publicIp' | head -n 1)

    # Add a check for failure states
    if [[ "$vm_status" == "ERROR" || "$vm_status" == "FAILED" ]]; then
        log_error "VM '$VM_NAME' entered status '$vm_status'. Aborting."
    fi
  done
  echo >&2 # Newline after progress dots

  log_success "VM is ACTIVE! Public IP Address: $public_ip"
}

# Waits for the SSH server to be ready on the VM
# Globals:
#   SSH_TIMEOUT
#   SSH_CHECK_INTERVAL
#   SSH_KEY_NAME
#   SSH_USER
# Arguments:
#   $1: public_ip
# Outputs:
#   None
wait_for_ssh_ready() {
  local public_ip="$1"

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
}

# This function defines the setup script that will be run on the remote VM
# Arguments:
#   $1: public_ip
#   $2: network_id
#   $3: sa_key_b64
#   $4: k8s_version
#   $5: project_id
#   $6: deploy_branch
#   $7: deploy_repo_url
get_kubeadm_script() {
  local public_ip="$1"
  local network_id="$2"
  local sa_key_b64="$3"
  local k8s_version="$4"
  local project_id="$5"
  local deploy_branch="$6"
  local deploy_repo_url="$7"
  
  # This check runs locally, ensuring arguments are set before generating the script
  [[ -n "$public_ip" ]] || log_error "Internal script error: public_ip is not set."
  [[ -n "$network_id" ]] || log_error "Internal script error: network_id is not set."
  [[ -n "$sa_key_b64" ]] || log_error "Internal script error: sa_key_b64 is not set."
  [[ -n "$k8s_version" ]] || log_error "Internal script error: k8s_version is not set."
  [[ -n "$project_id" ]] || log_error "Internal script error: project_id is not set."
  [[ -n "$deploy_repo_url" ]] || log_error "Internal script error: deploy_repo_url is not set."
  # deploy_branch can be empty, so no check for it

cat << EOF
#!/bin/bash
set -eo pipefail # Exit on error

log() {
  # Use \$* (escaped) to print all remote arguments as a single string
  printf "[KUBE] --- %s\n" "\$*"
}

log "Starting Kubernetes single-node setup..."
# Use the k8s_version passed as an argument
export K8S_VERSION="$k8s_version"

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
log "Installing Kubernetes components (v$k8s_version)..."
sudo apt-get update
sudo apt-get install -y apt-transport-https ca-certificates curl gpg jq git

# Create a stable path for the key
K8S_APT_KEYRING="/etc/apt/keyrings/kubernetes-apt-keyring.gpg"
# Extract major and minor version for repository URL (e.g., 1.29 from 1.29.0)
K8S_MAJOR_MINOR="${k8s_version%.*}"
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

sudo apt-get update
# Pin the version
sudo apt-get install -y kubelet="${k8s_version}-*" kubeadm="${k8s_version}-*" kubectl="${k8s_version}-*"
sudo apt-mark hold kubelet kubeadm kubectl

# 5. Initialize the cluster (IDEMPOTENCY CHECK)
if [ ! -f /etc/kubernetes/admin.conf ]; then
  log "Initializing cluster with kubeadm..."
  # The $public_ip variable is expanded by the *local* script's 'cat'
  sudo kubeadm init --pod-network-cidr=192.168.0.0/16 --kubernetes-version="$k8s_version" \
    --control-plane-endpoint="$public_ip" \
    --apiserver-cert-extra-sans="$public_ip" \
    --skip-certificate-key-print \
    --skip-token-print

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
  CALICO_OPERATOR_URL="https://raw.githubusercontent.com/projectcalico/calico/v3.28.0/manifests/tigera-operator.yaml"
  CALICO_RESOURCES_URL="https://raw.githubusercontent.com/projectcalico/calico/v3.28.0/manifests/custom-resources.yaml"
  
  log "Installing Calico CNI (Operator) from \${CALICO_OPERATOR_URL}..."
  kubectl create -f "\${CALICO_OPERATOR_URL}"

  log "Waiting for CRDs to be established..."
  kubectl create -f "\${CALICO_OPERATOR_URL}" --dry-run=client -o json | \
    jq -r 'select(.kind == "CustomResourceDefinition") | "crd/" + .metadata.name' | \
    xargs kubectl wait --for=condition=established --timeout=300s

  log "Waiting for Tigera Operator deployment to be ready..."
  kubectl wait deployment/tigera-operator -n tigera-operator --for=condition=available --timeout=300s

  log "Installing Calico CNI (Custom Resources) from \${CALICO_RESOURCES_URL}..."
  kubectl create -f "\${CALICO_RESOURCES_URL}"
else
  log "Calico operator (tigera-operator) already exists. Skipping CNI installation."
fi

# 8. Untaint the node to allow pods to run on the control-plane (IDEMPOTENCY CHECK)
# Wait for node to be ready before untainting (after CNI is installed)
log "Waiting for node to be ready after CNI installation..."
kubectl wait --for=condition=Ready node --all --timeout=300s

# Check if the node has the control-plane taint before trying to remove it
if kubectl get nodes -o json | jq -e '.items[0].spec.taints[] | select(.key == "node-role.kubernetes.io/control-plane")' >/dev/null 2>&1; then
  log "Untainting control-plane node..."
  kubectl taint nodes --all node-role.kubernetes.io/control-plane-
fi

# 9. Create ConfigMap and Secret for cloud-provider-stackit
log "Ensuring kube-system namespace exists..."
kubectl create namespace kube-system --dry-run=client -o yaml | kubectl apply -f -

log "Creating stackit-cloud-controller-manager ConfigMap..."
# The $project_id and $network_id are expanded by the *local*
# script's 'cat' command and embedded *as static values* here.
cat <<EOT_CM | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: stackit-cloud-config
  namespace: kube-system
data:
  cloud.yaml: |-
    projectId: $project_id
    networkId: $network_id
    region: eu01
  cloud.conf: |-
    [Global]
    project-id = $project_id
    [BlockStorage]
    node-volume-attach-limit = 20
    rescan-on-resize = true
EOT_CM
log "ConfigMap stackit-cloud-controller-manager created in kube-system."

log "Creating stackit-cloud-provider-credentials secret..."
# The $sa_key_b64 is expanded by the *local* script's 'cat'
# and embedded *as a static value* here.
cat <<EOT_SECRET | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: stackit-cloud-secret
  namespace: kube-system
type: Opaque
data:
  sa_key.json: $sa_key_b64
EOT_SECRET
log "Secret stackit-cloud-provider-credentials created."

# 10. Apply Kustomization
log "Installing cloud-provider-stackit..."
TARGET_BRANCH=""
RELEASE_BRANCH="release-v\${K8S_MAJOR_MINOR}"

# Use the $deploy_branch argument
if [ -n "${deploy_branch}" ]; then
  log "Using override branch from DEPLOY_BRANCH: ${deploy_branch}"
  TARGET_BRANCH="${deploy_branch}"
else
  log "Checking for release branch: \${RELEASE_BRANCH}..."
  # Use 'git ls-remote' to check if the branch exists on the remote
  # Use the $deploy_repo_url argument
  if git ls-remote --exit-code --heads "${deploy_repo_url}" "\${RELEASE_BRANCH}" &>/dev/null; then
    log "Found release branch: \${RELEASE_BRANCH}"
    TARGET_BRANCH="\${RELEASE_BRANCH}"
  else
    log "Release branch \${RELEASE_BRANCH} not found. Defaulting to 'main' branch."
    TARGET_BRANCH="main"
  fi
fi

log "Applying kustomization from branch: \${TARGET_BRANCH}"
# Use the -k URL with the ?ref= query parameter
# Use the $deploy_repo_url argument
kubectl apply -k "${deploy_repo_url}/deploy/cloud-controller-manager?ref=\${TARGET_BRANCH}"
# Patch the deployment to use Recreate strategy and set replicas to 1
kubectl patch deployment stackit-cloud-controller-manager -n kube-system --type='json' -p='[
  {"op": "replace", "path": "/spec/strategy/type", "value": "Recreate"},
  {"op": "remove", "path": "/spec/strategy/rollingUpdate"},
  {"op": "replace", "path": "/spec/replicas", "value": 1}
]'

log "Waiting for cloud-controller-manager to be ready..."
kubectl wait deployment/stackit-cloud-controller-manager -n kube-system --for=condition=available --timeout=300s

# Use the $deploy_repo_url argument
kubectl apply -k "${deploy_repo_url}/deploy/csi-plugin?ref=\${TARGET_BRANCH}"
log "Kustomization applied successfully."

log "✅ Kubernetes single-node cluster setup script finished."
log "Wait a minute for pods to come up, then check with 'kubectl get nodes -o wide' and 'kubectl get pods -A'."
EOF
}

# Executes the Kubeadm setup script on the remote VM
# Globals:
#   SSH_KEY_NAME
#   SSH_USER
#   K8S_VERSION
#   PROJECT_ID
#   DEPLOY_BRANCH
#   DEPLOY_REPO_URL
# Arguments:
#   $1: public_ip
#   $2: network_id
#   $3: sa_key_b64
# Outputs:
#   None
configure_kubeadm_node() {
  local public_ip="$1"
  local network_id="$2"
  local sa_key_b64="$3"
  
  log "Setting up Kubernetes on the VM..."
  local setup_script
  # Pass all required values as arguments
  setup_script=$(get_kubeadm_script \
    "$public_ip" \
    "$network_id" \
    "$sa_key_b64" \
    "$K8S_VERSION" \
    "$PROJECT_ID" \
    "$DEPLOY_BRANCH" \
    "$DEPLOY_REPO_URL")

  # Pass the script content as a command to SSH
  ssh -o "StrictHostKeyChecking=no" -o "IdentitiesOnly=yes" -i "$HOME/.ssh/$SSH_KEY_NAME" "$SSH_USER@$public_ip" "$setup_script"
}

# Prints the final access instructions
# Globals:
#   SSH_KEY_NAME
#   SSH_USER
#   KUBECONFIG_PATH
# Arguments:
#   $1: public_ip
# Outputs:
#   None
print_access_instructions() {
  local public_ip="$1"
  
  log_success "Kubernetes setup completed!"
  log "You can now access your cluster:"
  echo >&2
  echo "  ssh -o StrictHostKeyChecking=no -o IdentitiesOnly=yes -i $HOME/.ssh/$SSH_KEY_NAME $SSH_USER@$public_ip" >&2
  echo "  (Once inside: kubectl get nodes)" >&2
  echo >&2
  echo "To get the kubeconfig for local use:" >&2
  echo "  ssh -o StrictHostKeyChecking=no -o IdentitiesOnly=yes -i $HOME/.ssh/$SSH_KEY_NAME $SSH_USER@$public_ip 'cat .kube/config' > $KUBECONFIG_PATH" >&2
  echo "  KUBECONFIG=$KUBECONFIG_PATH kubectl get nodes" >&2
}


# --- Resource Creation (Controller) ---

create_resources() {
  log "Starting STACKIT VM & Kubeadm setup..."
  log "Project: $PROJECT_ID, VM: $VM_NAME, K8s: $K8S_VERSION"

  # 1. Prepare prerequisites in STACKIT
  log "Setting up prerequisites in STACKIT..."
  setup_ssh_key
  
  local network_id
  network_id=$(ensure_network)
  
  local image_id
  image_id=$(find_image_id)

  # 2. Setup Service Account and Security Group
  local sa_key_b64
  sa_key_b64=$(ensure_service_account)
  
  local security_group_id
  security_group_id=$(ensure_security_group)

  # 3. Create Server and IP
  local server_id
  server_id=$(ensure_server_instance "$network_id" "$security_group_id" "$image_id")
  
  local public_ip
  public_ip=$(ensure_public_ip "$server_id")

  # 4. Wait for Server to be ready
  wait_for_vm_ready "$server_id" "$public_ip"
  wait_for_ssh_ready "$public_ip"
  
  # 5. Copy and execute the Kubeadm setup script
  configure_kubeadm_node "$public_ip" "$network_id" "$sa_key_b64"

  # 6. Print access instructions
  print_access_instructions "$public_ip"
}

# --- Cleanup Functions ---

# Deletes all resources created by this script
cleanup_resources() {
  log "Starting cleanup of resources for project $PROJECT_ID..."

  # Load inventory to get resource IDs
  local inventory
  inventory=$(load_inventory)

  # 1. Delete the VM
  local server_id
  server_id=$(echo "$inventory" | jq -r '.server?.id')
  local server_name
  server_name=$(echo "$inventory" | jq -r '.server?.name')

  if [[ -n "$server_id" && "$server_id" != "null" ]]; then
    log "Found server '$server_name' (ID: $server_id) in inventory. Deleting..."
    if ! stackit server delete "$server_id" --project-id "$PROJECT_ID" -y; then
      log_warn "Failed to delete server '$server_name'. You may need to delete it manually."
    else
      log_success "Server '$server_name' deleted successfully."
    fi
  else
    log "No server ID found in inventory. Skipping server deletion."
  fi

  # 2. Delete the SSH key
  local ssh_key_name
  ssh_key_name=$(echo "$inventory" | jq -r '.ssh_key?.name')

  if [[ -n "$ssh_key_name" && "$ssh_key_name" != "null" ]]; then
    log "Found SSH key '$ssh_key_name' in inventory. Deleting..."
    if ! stackit key-pair delete "$ssh_key_name" --project-id "$PROJECT_ID" -y; then
      log_warn "Failed to delete SSH key '$ssh_key_name'. You may need to delete it manually."
    else
      log_success "SSH key '$ssh_key_name' deleted successfully."
    fi
  else
    log "No SSH key found in inventory. Skipping SSH key deletion."
  fi

  # 3. Delete the public IP
  local public_ip_id
  local public_ip
  public_ip_id=$(echo "$inventory" | jq -r '.public_ip?.id')
  public_ip=$(echo "$inventory" | jq -r '.public_ip?.name')

  if [[ -n "$public_ip_id" && "$public_ip_id" != "null" ]]; then
    log "Found public IP '$public_ip' (ID: $public_ip_id) in inventory. Deleting..."
    if ! stackit public-ip delete "$public_ip_id" --project-id "$PROJECT_ID" -y; then
      log_warn "Failed to delete public IP '$public_ip'. You may need to delete it manually."
    else
      log_success "Public IP '$public_ip' deleted successfully."
    fi
  else
    log "No public IP ID found in inventory. Skipping public IP deletion."
  fi

  # 4. Delete the Service Account
  local sa_email
  # The email is stored in the 'id' field of the inventory
  sa_email=$(echo "$inventory" | jq -r '.service_account?.id')

  if [[ -n "$sa_email" && "$sa_email" != "null" ]]; then
    log "Found service account '$sa_email' in inventory. Deleting..."
    if ! stackit service-account delete "$sa_email" --project-id "$PROJECT_ID" -y; then
      log_warn "Failed to delete service account '$sa_email'. You may need to delete it manually."
    else
      log_success "Service account '$sa_email' deleted successfully."
    fi
  else
    log "No service account found in inventory. Skipping service account deletion."
  fi

  # 5. Delete the security group
  local security_group_id
  local security_group_name
  security_group_id=$(echo "$inventory" | jq -r '.security_group?.id')
  security_group_name=$(echo "$inventory" | jq -r '.security_group?.name')

  if [[ -n "$security_group_id" && "$security_group_id" != "null" ]]; then
    log "Found security group '$security_group_name' (ID: $security_group_id) in inventory. Deleting..."
    if ! stackit security-group delete "$security_group_id" --project-id "$PROJECT_ID" -y; then
      log_warn "Failed to delete security group '$security_group_name'. You may need to delete it manually."
    else
      log_success "Security group '$security_group_name' deleted successfully."
    fi
  else
    log "No security group ID found in inventory. Skipping security group deletion."
  fi

  # 6. Delete the network
  local network_id
  network_id=$(echo "$inventory" | jq -r '.network?.id')

  if [[ -n "$network_id" && "$network_id" != "null" ]]; then
    log "Found network in inventory (ID: $network_id). Deleting..."
    if ! stackit network delete "$network_id" --project-id "$PROJECT_ID" -y; then
      log_warn "Failed to delete network. You may need to delete it manually."
    else
      log_success "Network deleted successfully."
    fi
  else
    log "No network ID found in inventory. Skipping network deletion."
  fi

  # 7. Clean up local files
  if [[ -f "$SA_KEY_PATH" ]]; then
    rm "$SA_KEY_PATH"
    log_success "Removed local service account key file."
  fi

  if [[ -f "$INVENTORY_FILE" ]]; then
    rm "$INVENTORY_FILE"
    log_success "Removed inventory file."
  fi

  if [[ -f "$KUBECONFIG_PATH" ]]; then
    rm "$KUBECONFIG_PATH"
    log_success "Remove kubeadm cluster kubeconfig."
  fi

  log_success "Cleanup process completed."
}

# --- Main Execution ---

main() {
  case "$ACTION" in
    create)
      check_deps
      check_auth
      create_resources
      ;;
    destroy)
      check_deps
      check_auth
      cleanup_resources
      ;;
    *)
      log_error "Usage: $0 <create|destroy> <PROJECT_ID> <K8S_VERSION>"
      ;;
  esac
}

main "$@"
