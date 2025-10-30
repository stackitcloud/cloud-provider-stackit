#!/usr/bin/env bash
set -eo pipefail # Exit on error, and exit on command failure in pipelines

# --- Configuration ---
# !! SCRIPT PARAMETERS:
#    $1: Action (create|destroy)
#    $2: Your STACKIT Project ID
#    $3: The Kubernetes version to install (e.g., "1.29.0")

ACTION="$1"
PROJECT_ID="$2"
NETWORK_ID=""    # Will be populated by find_network_id
SA_KEY_B64=""    # Will be populated by create_resources
PUBLIC_IP=""     # Will be populated by create_resources
K8S_VERSION="$3" # Example: "1.29.0"

# --- Script Configuration ---
VM_NAME="stackit-ccm-test"
# Can be overridden by environment variable (e.g. SSH_KEY_NAME="my-key" ./script.sh ...)
SSH_KEY_NAME="${SSH_KEY_NAME:-$VM_NAME}"
# Can be overridden by environment variable
NETWORK_NAME="${NETWORK_NAME:-$VM_NAME}"
# This script uses a 2-vCPU, 4GB-RAM machine type.
# You can find other types in the STACKIT Portal or documentation.
MACHINE_TYPE="c2i.2"
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

# Finds a network ID by name, using a default name if NETWORK_NAME is not set.
# Creates the network if it doesn't exist (only applicable for the default name).
find_network_id() {
  log "Finding a network..."
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
# It reads globals: $K8S_VERSION, $PROJECT_ID, $NETWORK_ID, $SA_KEY_B64, $DEPLOY_BRANCH, $DEPLOY_REPO_URL
get_kubeadm_script() {
  # This check runs locally, ensuring globals are set before generating the script
  [[ -n "$NETWORK_ID" ]] || log_error "Internal script error: NETWORK_ID is not set."
  [[ -n "$SA_KEY_B64" ]] || log_error "Internal script error: SA_KEY_B64 is not set."

cat << EOF
#!/bin/bash
set -eo pipefail # Exit on error

log() {
  # Use \$* (escaped) to print all remote arguments as a single string
  printf "[KUBE] --- %s\n" "\$*"
}

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
sudo apt-get install -y apt-transport-https ca-certificates curl gpg jq git

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

sudo apt-get update
# Pin the version
sudo apt-get install -y kubelet="${K8S_VERSION}-*" kubeadm="${K8S_VERSION}-*" kubectl="${K8S_VERSION}-*"
sudo apt-mark hold kubelet kubeadm kubectl

# 5. Initialize the cluster (IDEMPOTENCY CHECK)
if [ ! -f /etc/kubernetes/admin.conf ]; then
  log "Initializing cluster with kubeadm..."
  # Note: Using Calico's default CIDR
  sudo kubeadm init --pod-network-cidr=192.168.0.0/16 --kubernetes-version="$K8S_VERSION" \
    --control-plane-endpoint="$PUBLIC_IP" \
    --apiserver-cert-extra-sans="$PUBLIC_IP" \
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
    xargs kubectl wait --for=condition=established --timeout=60s

  log "Waiting for Tigera Operator deployment to be ready..."
  kubectl wait deployment/tigera-operator -n tigera-operator --for=condition=available --timeout=120s

  log "Installing Calico CNI (Custom Resources) from \${CALICO_RESOURCES_URL}..."
  kubectl create -f "\${CALICO_RESOURCES_URL}"
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

# 9. Create ConfigMap and Secret for cloud-provider-stackit
log "Ensuring kube-system namespace exists..."
kubectl create namespace kube-system --dry-run=client -o yaml | kubectl apply -f -

log "Creating stackit-cloud-controller-manager ConfigMap..."
# Use a Here-Document *inside* the remote script to create the ConfigMap
# The variables $PROJECT_ID and $NETWORK_ID are expanded by the *local*
# script's 'cat' command and embedded *as static values* here.
cat <<EOT_CM | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: stackit-cloud-config
  namespace: kube-system
data:
  cloud.yaml: |-
    projectId: $PROJECT_ID
    networkId: $NETWORK_ID
    region: eu01
  cloud.conf: |-
    [Global]
    project-id = $PROJECT_ID
    [BlockStorage]
    node-volume-attach-limit = 20
    rescan-on-resize = true
EOT_CM
log "ConfigMap stackit-cloud-controller-manager created in kube-system."

log "Creating stackit-cloud-provider-credentials secret..."
# The $SA_KEY_B64 is expanded by the *local* script's 'cat'
# and embedded *as a static value* here.
cat <<EOT_SECRET | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: stackit-cloud-secret
  namespace: kube-system
type: Opaque
data:
  sa_key.json: $SA_KEY_B64
EOT_SECRET
log "Secret stackit-cloud-provider-credentials created."

# 10. Apply Kustomization
log "Installing cloud-provider-stackit..."
TARGET_BRANCH=""
RELEASE_BRANCH="release-v\${K8S_MAJOR_MINOR}"

if [ -n "${DEPLOY_BRANCH}" ]; then
  log "Using override branch from DEPLOY_BRANCH: ${DEPLOY_BRANCH}"
  TARGET_BRANCH="${DEPLOY_BRANCH}"
else
  log "Checking for release branch: \${RELEASE_BRANCH}..."
  # Use 'git ls-remote' to check if the branch exists on the remote
  # Exit code 0 = exists, 2 = not found
  if git ls-remote --exit-code --heads "${DEPLOY_REPO_URL}" "\${RELEASE_BRANCH}" &>/dev/null; then
    log "Found release branch: \${RELEASE_BRANCH}"
    TARGET_BRANCH="\${RELEASE_BRANCH}"
  else
    log "Release branch \${RELEASE_BRANCH} not found. Defaulting to 'main' branch."
    TARGET_BRANCH="main"
  fi
fi

log "Applying kustomization from branch: \${TARGET_BRANCH}"
# Use the -k URL with the ?ref= query parameter
# Apply the cloud-controller-manager
kubectl apply -k "${DEPLOY_REPO_URL}/deploy/cloud-controller-manager?ref=\${TARGET_BRANCH}"
# Patch the deployment to use Recreate strategy and set replicas to 1
kubectl patch deployment stackit-cloud-controller-manager -n kube-system --type='json' -p='[
  {"op": "replace", "path": "/spec/strategy/type", "value": "Recreate"},
  {"op": "remove", "path": "/spec/strategy/rollingUpdate"},
  {"op": "replace", "path": "/spec/replicas", "value": 1}
]'
kubectl apply -k "${DEPLOY_REPO_URL}/deploy/csi-plugin?ref=\${TARGET_BRANCH}"
log "Kustomization applied successfully."

log "✅ Kubernetes single-node cluster setup script finished."
log "Wait a minute for pods to come up, then check with 'kubectl get nodes -o wide' and 'kubectl get pods -A'."
EOF
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

# Get resource ID from inventory
get_from_inventory() {
  local resource_type="$1"
  local inventory
  inventory=$(load_inventory)
  echo "$inventory" | jq -r --arg type "$resource_type" '.[$type]?.id'
}

# --- Resource Creation ---

create_resources() {
  log "Starting STACKIT VM & Kubeadm setup..."
  log "Project: $PROJECT_ID, VM: $VM_NAME, K8s: $K8S_VERSION"

  # 1. Prepare prerequisites in STACKIT
  log "Setting up prerequisites in STACKIT..."
  setup_ssh_key
  NETWORK_ID=$(find_network_id)
  local image_id
  image_id=$(find_image_id)

  # 2. Setup Service Account and Key
  local sa_name=$VM_NAME
  sa_json=$(stackit service-account list --project-id "$PROJECT_ID" --output-format json)
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
  
  # Assign to global SA_KEY_B64. Use -w 0 for no line wraps.
  SA_KEY_B64=$(echo -n "$sa_key_json" | base64 -w 0)
  [[ -n "$SA_KEY_B64" ]] || log_error "Failed to base64 encode SA key."
  log "Service account key encoded."

  # 3. Setup security group for SSH
  log "Setting up security group for SSH..."
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
    stackit security-group rule create --security-group-id "$security_group_id" \
      --direction ingress --protocol-name tcp --port-range-max 22 --port-range-min 22 \
      --description "SSH Access" --project-id "$PROJECT_ID" -y
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
    stackit security-group rule create --security-group-id "$security_group_id" \
      --direction ingress --protocol-name tcp --port-range-max 6443 --port-range-min 6443 \
      --description "API Access" --project-id "$PROJECT_ID" -y
    log_success "Added API rule to security group '$security_group_name'"
  else
    log_success "API rule already exists in security group '$security_group_name'"
  fi

  # 4. Check if server already exists
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
      --network-id "$NETWORK_ID" \
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

  # 5. Check for Public IP and attach if missing
  log "Setting up Public IP for server '$VM_NAME'..."
  local current_server_details
  current_server_details=$(stackit server describe "$server_id" --project-id "$PROJECT_ID" --output-format json)
  local existing_ip
  existing_ip=$(echo "$current_server_details" | jq -r '.nics[] | select(.publicIp != null) | .publicIp' | head -n 1)

  if [[ -n "$existing_ip" && "$existing_ip" != "null" ]]; then
    PUBLIC_IP="$existing_ip"
    log "Using existing Public IP $PUBLIC_IP from server '$VM_NAME'."
  else
    log "Creating a new Public IP..."
    local public_ip_json
    public_ip_json=$(stackit public-ip create -y --project-id "$PROJECT_ID" --output-format json)

    local pip_create_exit_code=$?
    if [[ $pip_create_exit_code -ne 0 ]]; then
        log_error "Failed to execute 'stackit public-ip create'. Exit code: $pip_create_exit_code. Output: $public_ip_json"
    fi
    PUBLIC_IP=$(echo "$public_ip_json" | jq -r '.ip')
    public_ip_id=$(echo "$public_ip_json" | jq -r '.id')
    if [[ -z "$PUBLIC_IP" || "$PUBLIC_IP" == "null" ]]; then
        log_error "Failed to extract IP from public IP creation output: $public_ip_json"
    fi
    log_success "Created Public IP: $PUBLIC_IP"
    update_inventory "public_ip" "$public_ip_id" "$PUBLIC_IP"
  fi

  # Check if the public IP is already attached to the server
  local attached_ip
  attached_ip=$(echo "$current_server_details" | jq -r --arg target_ip "$PUBLIC_IP" '.nics[] | select(.publicIp != null and .publicIp == $target_ip) | .publicIp' | head -n 1)

  if [[ "$attached_ip" == "$PUBLIC_IP" ]]; then
    log "Public IP $PUBLIC_IP already attached to server '$VM_NAME'."
  elif [[ -n "$attached_ip" && "$attached_ip" != "null" ]]; then
    # A *different* IP is attached. This is unexpected. Error out.
    log_error "Server '$VM_NAME' already has a different Public IP attached $attached_ip. Cannot attach $PUBLIC_IP."
  else
    # No IP or expected IP not attached, proceed with attach
    log "Attaching Public IP $PUBLIC_IP to server $server_id..."

    # We need the ID of the public IP to attach it
    local public_ip_id
    public_ip_id=$(stackit public-ip list --project-id "$PROJECT_ID" --output-format json | \
      jq -r --arg ip "$PUBLIC_IP" 'map(select(.ip == $ip)) | .[0].id')

    if [[ -z "$public_ip_id" || "$public_ip_id" == "null" ]]; then
        log_error "Could not find Public IP ID for IP $PUBLIC_IP."
    fi

    stackit server public-ip attach "$public_ip_id" --server-id "$server_id" --project-id "$PROJECT_ID" -y
    local attach_exit_code=$?
    if [[ $attach_exit_code -ne 0 ]]; then
        log_error "Failed to attach Public IP $public_ip_id to server $server_id. Exit code: $attach_exit_code."
    fi
    log_success "Public IP attach command sent."
  fi

  # 6. Wait for the server to be "ACTIVE"
  log "Waiting for VM '$VM_NAME' (ID: $server_id) to become 'ACTIVE' and IP to appear..."
  local vm_status="" # Reset status before loop
  local ip_attached=""
  local security_group_id=""

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
    ip_attached=$(echo "$vm_details" | jq -r --arg target_ip "$PUBLIC_IP" '.nics[] | select(.publicIp != null and .publicIp == $target_ip) | .publicIp' | head -n 1)

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

  log_success "VM is ACTIVE! Public IP Address: $PUBLIC_IP"

  # 7. Wait for SSH to be ready
  log "Waiting for SSH server to be ready on $PUBLIC_IP..."
  local ssh_ready=false
  local elapsed_time=0

  while [[ $elapsed_time -lt $SSH_TIMEOUT ]]; do
    if ssh -o "StrictHostKeyChecking=no" -o "ConnectTimeout=5" -o "IdentitiesOnly=yes" -i "$HOME/.ssh/$SSH_KEY_NAME" "$SSH_USER@$PUBLIC_IP" "echo 'SSH is up'" &>/dev/null; then
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

  # 8. Copy and execute the Kubeadm setup script
  log "Setting up Kubernetes on the VM..."
  local setup_script
  setup_script=$(get_kubeadm_script)

  # Pass the script content as a command to SSH
  ssh -o "StrictHostKeyChecking=no" -o "IdentitiesOnly=yes" -i "$HOME/.ssh/$SSH_KEY_NAME" "$SSH_USER@$PUBLIC_IP" "$setup_script"

  log_success "Kubernetes setup completed!"
  log "You can now access your cluster:"
  echo >&2
  echo "  ssh -o StrictHostKeyChecking=no -o IdentitiesOnly=yes -i $HOME/.ssh/$SSH_KEY_NAME $SSH_USER@$PUBLIC_IP" >&2
  echo "  (Once inside: kubectl get nodes)" >&2
  echo >&2
  echo "To get the kubeconfig for local use:" >&2
  echo "  ssh -o StrictHostKeyChecking=no -o IdentitiesOnly=yes -i $HOME/.ssh/$SSH_KEY_NAME $SSH_USER@$PUBLIC_IP 'cat .kube/config' > $KUBECONFIG_PATH" >&2
  echo "  KUBECONFIG=$KUBECONFIG_PATH kubectl get nodes" >&2
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
