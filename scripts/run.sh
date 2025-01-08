#!/bin/bash

YC_TOKEN="${YC_TOKEN:-""}"
NAME_PREFIX="${NAME_PREFIX:-"my"}"
SSH_PASSPHRASE="${SSH_PASSPHRASE:-""}"
YC_ZONE="${YC_ZONE:-"ru-central1-a"}"
YC_CLOUD_ID="${YC_CLOUD_ID:-""}"
YC_FOLDER="${YC_FOLDER:-"default"}"

YC_PATH="$(pwd)"/yc
YC_BIN="${YC_PATH}/bin/yc"

echo "Installing yc..."
curl -sS https://storage.yandexcloud.net/yandexcloud-yc/install.sh | \
    bash -s -- -i "$YC_PATH" -n

echo "Configuring yc..."
$YC_BIN config set token "${YC_TOKEN}"
$YC_BIN config set cloud-id "${YC_CLOUD_ID}"
$YC_BIN config set folder-name "${YC_FOLDER}"

echo "Creating network..."
$YC_BIN vpc network create \
--name "${NAME_PREFIX}-net" \
--description "${NAME_PREFIX}-net"

echo "Creating subnet..."
$YC_BIN vpc subnet create \
--name "${NAME_PREFIX}-subnet" \
--range 192.168.0.0/24 \
--network-name "${NAME_PREFIX}-net" \
--zone "$YC_ZONE" \
--description "${NAME_PREFIX}-subnet"

echo "Generating SSH key..."
ssh-keygen -t ed25519 -C "" -N "$SSH_PASSPHRASE" -f "${NAME_PREFIX}-yc_key"

chmod 600 "${NAME_PREFIX}-yc_key"
chmod 600 "${NAME_PREFIX}-yc_key.pub"

echo "Removing instance if already exists..."
$YC_BIN compute instance delete "${NAME_PREFIX}-yc-instance"

echo "Creating new instance..."
$YC_BIN compute instance create \
  --name "${NAME_PREFIX}-yc-instance" \
  --network-interface subnet-name="${NAME_PREFIX}-subnet",nat-ip-version=ipv4 \
  --create-boot-disk name="${NAME_PREFIX}-osdisk",image-folder-id=standard-images,image-family=ubuntu-24-04-lts \
  --zone "$YC_ZONE" \
  --cores 2 \
  --memory 2 \
  --ssh-key "${NAME_PREFIX}-yc_key.pub"

echo "Fetching new instance IP..."
VM_IP="$($YC_BIN compute instance show --name "${NAME_PREFIX}"-yc-instance | grep -E ' +address' | tail -n 1 | awk '{print $2}')"

if [[ $VM_IP =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "IP: $VM_IP"
else
    echo "Failed fetch IP. Something went wrong."
    exit 1
fi

echo "Checking SSH connection..."
max_attempts=5
attempt=0
resp=""

while [[ $attempt -le $max_attempts ]]; do
  ((attempt++))
  resp=$(ssh -o StrictHostKeyChecking=no -i "${NAME_PREFIX}-yc_key" "yc-user@${VM_IP}" "echo ok")
  if [[ "$resp" = "ok" ]]; then
    echo "SSH test passed"
    break
  else
    echo "SSH connection test failed: ${resp}. Attempt $attempt of $max_attempts"
    if [[ $attempt -eq $max_attempts ]]; then
      exit 1
    fi
    sleep 15
  fi
done

echo "Deploying app..."

ssh -i "${NAME_PREFIX}-yc_key" "yc-user@${VM_IP}" "curl -fsSL https://get.docker.com -o get-docker.sh && \
    sudo sh ./get-docker.sh && \
    wget -O .env https://github.com/dvidvime/itmo-devops-sem1-project-template/raw/refs/heads/main/.env.example && \
    wget -O init.sql https://github.com/dvidvime/itmo-devops-sem1-project-template/raw/refs/heads/main/init.sql && \
    wget -O docker-compose.yml https://github.com/dvidvime/itmo-devops-sem1-project-template/raw/refs/heads/main/docker-compose.production.yml && \
    sudo docker compose up -d"

echo "YOUR SSH PRIVATE KEY:"
cat "${NAME_PREFIX}-yc_key"

echo "DONE! IP: ${VM_IP}"

sleep 10

echo "Testing..."

export REMOTE_HOST="${VM_IP}"

source "$(pwd)"/tests.sh 1
source "$(pwd)"/tests.sh 2
source "$(pwd)"/tests.sh 3