#!/bin/bash
# Transfer Testing Script
# Tests qui's torrent transfer functionality using qBittorrent containers
#
# Usage:
#   ./run-test.sh          - Run full test (cleans up after completion)
#   ./run-test.sh --keep   - Run test and keep containers running for debugging
#   ./run-test.sh cleanup  - Stop everything and clean up

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QUI_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$SCRIPT_DIR"

# Default: cleanup after tests
KEEP_RUNNING=false

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[+]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[-]${NC} $1"; exit 1; }

# Cleanup function
do_cleanup() {
    log "Cleaning up..."
    pkill -f "qui-test.*serve" 2>/dev/null || true
    docker compose -f docker-compose.yml down -v 2>/dev/null || true
    rm -rf testdata /tmp/qui-cookies 2>/dev/null || true
    log "Cleanup complete"
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --keep|-k)
            KEEP_RUNNING=true
            shift
            ;;
        cleanup)
            do_cleanup
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--keep] | cleanup"
            exit 1
            ;;
    esac
done

# Trap to ensure cleanup on exit (unless --keep is specified)
cleanup_on_exit() {
    local exit_code=$?
    if [ "$KEEP_RUNNING" = false ]; then
        log "Cleaning up test environment..."
        do_cleanup
    fi
    exit $exit_code
}
trap cleanup_on_exit EXIT

#
# STEP 0: Cleanup previous run
#
log "Step 0: Cleaning up previous test artifacts..."
pkill -f "qui-test.*serve" 2>/dev/null || true
docker compose -f docker-compose.yml down -v 2>/dev/null || true
rm -rf testdata /tmp/qui-cookies 2>/dev/null || true

#
# STEP 1: Generate SSH keys if needed
#
log "Step 1: Checking SSH keys..."
if [ ! -f "ssh-keys/test_key" ]; then
    mkdir -p ssh-keys
    ssh-keygen -t ed25519 -f ssh-keys/test_key -N "" -C "qui-test" -q
    log "Generated SSH keys"
else
    log "SSH keys exist"
fi

#
# STEP 2: Compile qui
#
log "Step 2: Compiling qui..."
cd "$QUI_ROOT"
go build -o "$SCRIPT_DIR/qui-test" ./cmd/qui
cd "$SCRIPT_DIR"
log "Compiled: $SCRIPT_DIR/qui-test"

#
# STEP 3: Start qBittorrent containers (host network mode)
#
log "Step 3: Starting qBittorrent containers..."
docker compose -f docker-compose.yml up -d
log "Waiting for containers to initialize..."
sleep 15

#
# STEP 4: Get qBittorrent passwords from logs
# IMPORTANT: Do this BEFORE starting qui to avoid ban from wrong passwords
#
log "Step 4: Getting qBittorrent passwords..."
PW1=$(docker logs qbit1 2>&1 | grep -o 'temporary password.*: [^ ]*' | tail -1 | awk '{print $NF}')
PW2=$(docker logs qbit2 2>&1 | grep -o 'temporary password.*: [^ ]*' | tail -1 | awk '{print $NF}')
PW3=$(docker logs qbit3 2>&1 | grep -o 'temporary password.*: [^ ]*' | tail -1 | awk '{print $NF}')

[ -z "$PW1" ] && error "Could not get password for qbit1"
[ -z "$PW2" ] && error "Could not get password for qbit2"
[ -z "$PW3" ] && error "Could not get password for qbit3"

log "qbit1 password: $PW1"
log "qbit2 password: $PW2"
log "qbit3 password: $PW3"

# Verify passwords work before proceeding
log "Verifying qBittorrent credentials..."
for port in 8081 8082 8083; do
    case $port in
        8081) pw="$PW1" ;;
        8082) pw="$PW2" ;;
        8083) pw="$PW3" ;;
    esac
    result=$(curl -s "http://127.0.0.1:$port/api/v2/auth/login" -d "username=admin&password=$pw")
    if [ "$result" != "Ok." ]; then
        error "qbit on port $port login failed: $result"
    fi
done
log "All qBittorrent credentials verified"

#
# STEP 5: Install and configure SSH in containers
#
log "Step 5: Configuring SSH in containers..."
PUBKEY=$(cat ssh-keys/test_key.pub)

for i in 1 2 3; do
    SSH_PORT=$((2220 + i))
    log "Setting up qbit$i SSH on port $SSH_PORT..."

    # Install packages
    docker exec qbit$i sh -c "apk update && apk add openssh rsync" > /dev/null 2>&1

    # Generate host keys
    docker exec qbit$i ssh-keygen -A > /dev/null 2>&1

    # Setup authorized_keys and private key (for rsync between containers)
    docker exec qbit$i sh -c "mkdir -p /root/.ssh && chmod 700 /root/.ssh"
    docker exec qbit$i sh -c "echo '$PUBKEY' > /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys"
    docker cp ssh-keys/test_key qbit$i:/root/.ssh/id_ed25519
    docker exec qbit$i chmod 600 /root/.ssh/id_ed25519
    # Disable strict host key checking for rsync
    docker exec qbit$i sh -c "echo -e 'Host *\n  StrictHostKeyChecking no\n  UserKnownHostsFile /dev/null' > /root/.ssh/config && chmod 600 /root/.ssh/config"

    # Configure sshd with unique port
    docker exec qbit$i sh -c "sed -i 's/#Port 22/Port $SSH_PORT/' /etc/ssh/sshd_config"
    docker exec qbit$i sh -c "sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config"
    docker exec qbit$i sh -c "sed -i 's/#PubkeyAuthentication.*/PubkeyAuthentication yes/' /etc/ssh/sshd_config"

    # Unlock root account (required for SSH pubkey auth)
    docker exec qbit$i sh -c 'echo "root:testpass" | chpasswd'

    # Start sshd
    docker exec qbit$i /usr/sbin/sshd
done

# Verify SSH works
log "Verifying SSH connectivity..."
for i in 1 2 3; do
    SSH_PORT=$((2220 + i))
    if ! ssh -i ssh-keys/test_key -p $SSH_PORT -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 root@127.0.0.1 "echo ok" > /dev/null 2>&1; then
        error "SSH to qbit$i failed"
    fi
done
log "All SSH connections verified"

#
# STEP 6: Start qui
#
log "Step 6: Starting qui..."
mkdir -p testdata/config testdata/data

./qui-test --config-dir testdata/config generate-config > /dev/null 2>&1
./qui-test --config-dir testdata/config --data-dir testdata/data create-user --username test --password secret123 > /dev/null 2>&1
./qui-test --config-dir testdata/config --data-dir testdata/data serve > testdata/qui.log 2>&1 &
QUI_PID=$!

sleep 3
if ! kill -0 $QUI_PID 2>/dev/null; then
    error "qui failed to start. Check testdata/qui.log"
fi
log "qui started (PID: $QUI_PID)"

#
# STEP 7: Configure qui with instances
# Now safe to configure since we have correct passwords
#
log "Step 7: Configuring qui instances..."
QUI_API="http://localhost:7476/api"
SSH_KEY_PATH="$SCRIPT_DIR/ssh-keys/test_key"

# Login
curl -s -c /tmp/qui-cookies -X POST "$QUI_API/auth/login" \
    -H "Content-Type: application/json" \
    -d '{"username": "test", "password": "secret123"}' > /dev/null

# Create instances with correct passwords
ID1=$(curl -s -b /tmp/qui-cookies -X POST "$QUI_API/instances" \
    -H "Content-Type: application/json" \
    -d "{\"name\": \"qbit1\", \"host\": \"http://127.0.0.1:8081\", \"username\": \"admin\", \"password\": \"$PW1\", \"hasLocalFilesystemAccess\": false}" | jq -r '.id')
log "Created instance qbit1 (ID: $ID1)"

ID2=$(curl -s -b /tmp/qui-cookies -X POST "$QUI_API/instances" \
    -H "Content-Type: application/json" \
    -d "{\"name\": \"qbit2\", \"host\": \"http://127.0.0.1:8082\", \"username\": \"admin\", \"password\": \"$PW2\", \"hasLocalFilesystemAccess\": false}" | jq -r '.id')
log "Created instance qbit2 (ID: $ID2)"

ID3=$(curl -s -b /tmp/qui-cookies -X POST "$QUI_API/instances" \
    -H "Content-Type: application/json" \
    -d "{\"name\": \"qbit3\", \"host\": \"http://127.0.0.1:8083\", \"username\": \"admin\", \"password\": \"$PW3\", \"hasLocalFilesystemAccess\": false}" | jq -r '.id')
log "Created instance qbit3 (ID: $ID3)"

# Create SSH connections
curl -s -b /tmp/qui-cookies -X POST "$QUI_API/instances/$ID1/connections" \
    -H "Content-Type: application/json" \
    -d "{\"protocol\": \"ssh\", \"host\": \"127.0.0.1\", \"port\": 2221, \"username\": \"root\", \"privateKeyPath\": \"$SSH_KEY_PATH\", \"enabled\": true}" > /dev/null
curl -s -b /tmp/qui-cookies -X POST "$QUI_API/instances/$ID2/connections" \
    -H "Content-Type: application/json" \
    -d "{\"protocol\": \"ssh\", \"host\": \"127.0.0.1\", \"port\": 2222, \"username\": \"root\", \"privateKeyPath\": \"$SSH_KEY_PATH\", \"enabled\": true}" > /dev/null
curl -s -b /tmp/qui-cookies -X POST "$QUI_API/instances/$ID3/connections" \
    -H "Content-Type: application/json" \
    -d "{\"protocol\": \"ssh\", \"host\": \"127.0.0.1\", \"port\": 2223, \"username\": \"root\", \"privateKeyPath\": \"$SSH_KEY_PATH\", \"enabled\": true}" > /dev/null
log "Created SSH connections"

# Create path mappings (including /shared for hardlink tests)
curl -s -b /tmp/qui-cookies -X POST "$QUI_API/instances/$ID1/path-mappings" \
    -H "Content-Type: application/json" \
    -d '{"instancePath": "/downloads", "canonicalPath": "/downloads", "enabled": true}' > /dev/null
curl -s -b /tmp/qui-cookies -X POST "$QUI_API/instances/$ID1/path-mappings" \
    -H "Content-Type: application/json" \
    -d '{"instancePath": "/shared", "canonicalPath": "/shared", "enabled": true}' > /dev/null
curl -s -b /tmp/qui-cookies -X POST "$QUI_API/instances/$ID2/path-mappings" \
    -H "Content-Type: application/json" \
    -d '{"instancePath": "/downloads", "canonicalPath": "/downloads", "enabled": true}' > /dev/null
curl -s -b /tmp/qui-cookies -X POST "$QUI_API/instances/$ID2/path-mappings" \
    -H "Content-Type: application/json" \
    -d '{"instancePath": "/shared", "canonicalPath": "/shared", "enabled": true}' > /dev/null
curl -s -b /tmp/qui-cookies -X POST "$QUI_API/instances/$ID3/path-mappings" \
    -H "Content-Type: application/json" \
    -d '{"instancePath": "/downloads", "canonicalPath": "/downloads", "enabled": true}' > /dev/null
log "Created path mappings"

# Helper function to run a transfer and wait for completion
run_transfer() {
    local source_id=$1
    local target_id=$2
    local hash=$3
    local delete_source=${4:-false}
    local expected_mode=${5:-""}

    local result=$(curl -s -b /tmp/qui-cookies -X POST "$QUI_API/instances/$source_id/torrents/$hash/move" \
        -H "Content-Type: application/json" \
        -d "{\"targetInstanceId\": $target_id, \"deleteFromSource\": $delete_source}")
    local transfer_id=$(echo "$result" | jq -r '.id')

    if [ "$transfer_id" = "null" ] || [ -z "$transfer_id" ]; then
        echo "Failed to start transfer: $result"
        return 1
    fi

    echo -n "Transfer $transfer_id: "
    for attempt in $(seq 1 60); do
        local transfer=$(curl -s -b /tmp/qui-cookies "$QUI_API/transfers/$transfer_id")
        local state=$(echo "$transfer" | jq -r '.state')

        case "$state" in
            "completed")
                local mode=$(echo "$transfer" | jq -r '.linkMode')
                echo "completed (mode: $mode)"
                if [ -n "$expected_mode" ] && [ "$mode" != "$expected_mode" ]; then
                    echo "  WARNING: Expected mode '$expected_mode' but got '$mode'"
                    return 1
                fi
                return 0
                ;;
            "failed"|"cancelled")
                local err=$(echo "$transfer" | jq -r '.error')
                echo "failed: $err"
                return 1
                ;;
            *)
                echo -n "."
                sleep 2
                ;;
        esac
    done
    echo "timeout"
    return 1
}

#
# STEP 8: Add test torrents
#
log "Step 8: Adding test torrents..."
SID1=$(curl -s -c - "http://127.0.0.1:8081/api/v2/auth/login" -d "username=admin&password=$PW1" | grep SID | awk '{print $NF}')

# Add torrent to /downloads (for rsync test)
curl -s "http://127.0.0.1:8081/api/v2/torrents/add" -b "SID=$SID1" -F "torrents=@wired-cd.torrent" -F "savepath=/downloads" > /dev/null
HASH1="a88fda5954e89178c372716a6a78b8180ed4dad3"

# Add another torrent to /shared (for hardlink test)
curl -s "http://127.0.0.1:8081/api/v2/torrents/add" -b "SID=$SID1" -F "torrents=@sintel.torrent" -F "savepath=/shared" > /dev/null
HASH2="08ada5a7a6183aae1e09d831df6748d566095a10"

# Wait for downloads
log "Waiting for torrents to download..."
for hash in $HASH1 $HASH2; do
    echo -n "  $hash: "
    for attempt in $(seq 1 120); do
        state=$(curl -s "http://127.0.0.1:8081/api/v2/torrents/info?hashes=$hash" -b "SID=$SID1" | jq -r '.[0].state // "unknown"')
        if [ "$state" = "uploading" ] || [ "$state" = "stalledUP" ] || [ "$state" = "pausedUP" ]; then
            echo "done"
            break
        fi
        echo -n "."
        sleep 2
    done
done

TEST_FAILURES=0

#
# STEP 9: Test 1 - Basic rsync transfer (different filesystems)
#
log "Step 9a: Test rsync transfer (qbit1 -> qbit2, different volumes)..."
if run_transfer "$ID1" "$ID2" "$HASH1" "false" "transfer"; then
    log "  PASS: Rsync transfer"
else
    warn "  FAIL: Rsync transfer"
    ((TEST_FAILURES++))
fi

#
# STEP 9b: Test hardlink transfer (same filesystem)
#
log "Step 9b: Test hardlink transfer (qbit1 -> qbit2, shared volume)..."
# Note: This requires the instances to be configured with hasLocalFilesystemAccess=true
# and useHardlinks=true, but since they're marked as remote instances, this will use
# SSH-based hardlinking. For a true hardlink test, we'd need local filesystem access.
if run_transfer "$ID1" "$ID2" "$HASH2" "false"; then
    log "  PASS: Transfer on shared volume"
else
    warn "  FAIL: Transfer on shared volume"
    ((TEST_FAILURES++))
fi

#
# STEP 9c: Test deleteFromSource
#
log "Step 9c: Test deleteFromSource (qbit2 -> qbit3)..."
# First verify the torrent exists on qbit2
SID2=$(curl -s -c - "http://127.0.0.1:8082/api/v2/auth/login" -d "username=admin&password=$PW2" | grep SID | awk '{print $NF}')
torrent_count=$(curl -s "http://127.0.0.1:8082/api/v2/torrents/info?hashes=$HASH1" -b "SID=$SID2" | jq 'length')
if [ "$torrent_count" -eq 1 ]; then
    if run_transfer "$ID2" "$ID3" "$HASH1" "true" "transfer"; then
        # Verify torrent was removed from source
        sleep 2
        remaining=$(curl -s "http://127.0.0.1:8082/api/v2/torrents/info?hashes=$HASH1" -b "SID=$SID2" | jq 'length')
        if [ "$remaining" -eq 0 ]; then
            log "  PASS: deleteFromSource (torrent removed from qbit2)"
        else
            warn "  FAIL: Torrent still exists on qbit2 after deleteFromSource"
            ((TEST_FAILURES++))
        fi
    else
        warn "  FAIL: deleteFromSource transfer"
        ((TEST_FAILURES++))
    fi
else
    warn "  SKIP: Torrent not found on qbit2 (previous test may have failed)"
fi

#
# Done - Report results
#
echo ""
log "=========================================="
if [ $TEST_FAILURES -eq 0 ]; then
    log "All tests passed!"
else
    warn "$TEST_FAILURES test(s) failed"
fi
log "=========================================="

if [ "$KEEP_RUNNING" = true ]; then
    echo ""
    echo "qui is running at: http://localhost:7476"
    echo "qui PID: $QUI_PID"
    echo "Login: test / secret123"
    echo ""
    echo "qBittorrent instances:"
    echo "  - qbit1: http://127.0.0.1:8081 (admin / $PW1)"
    echo "  - qbit2: http://127.0.0.1:8082 (admin / $PW2)"
    echo "  - qbit3: http://127.0.0.1:8083 (admin / $PW3)"
    echo ""
    echo "To stop: ./run-test.sh cleanup"
    # Disable the exit trap cleanup since user wants to keep running
    trap - EXIT
fi

# Exit with failure if tests failed
if [ $TEST_FAILURES -gt 0 ]; then
    exit 1
fi
