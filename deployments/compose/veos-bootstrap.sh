#!/bin/bash
# veos-bootstrap.sh — install qemu, write a startup-config that enables
# eAPI, and launch vEOS-lab. Runs as the container entrypoint.
#
# Hardware needs:
#   - /dev/kvm passed in (compose.veos.yml has this).
#   - 2+ GB RAM, 2+ vCPU (qemu defaults).
#
# vEOS-lab management = Ethernet1 in the VM (mapped to first NIC).
# Exposed on the container's primary interface via slirp /
# user-mode networking with port redirects; the host reaches eAPI
# at http://127.0.0.1:18180/command-api.
set -euo pipefail

# tianon/qemu image already has qemu-system-x86_64 + qemu-img.
# Only mtools is missing (FAT32 startup-config disk packaging).
if ! command -v mcopy >/dev/null 2>&1; then
  apt-get update -qq
  apt-get install -qq -y --no-install-recommends mtools dosfstools >/dev/null
fi

QCOW=/qcow/vEOS64-lab-4.36.0.1F.qcow2
WORK=/var/lib/veos
mkdir -p "$WORK"

# Layered overlay so the read-only mount stays read-only.
qemu-img create -f qcow2 -F qcow2 -b "$QCOW" "$WORK/disk.qcow2" >/dev/null

# vEOS-lab boot story (after iteration with the live VM):
#   * Aboot looks for /mnt/flash/{zerotouch-config, startup-config}.
#   * If `zerotouch-config` is absent — vEOS hits the
#     ZTP→CloudVision loop forever (DHCPv4 → arista.io enrollment
#     → fail → retry); the VM never reaches eAPI without explicit
#     `zerotouch cancel` over the console.
#   * Solution: stage `zerotouch-config` with the literal token
#     `DISABLE` so first boot exits ZTP immediately.
#
# Both files (zerotouch-config + startup-config) are packed into a
# FAT32 disk image labelled CONFIG. EOS auto-mounts it as
# /mnt/usb1, then `Aboot` copies any *fresh* startup-config /
# zerotouch-config into /mnt/flash on first boot.
#
# Reference: arista-netdevops-community/kvm-lab-for-network-engineers
mkdir -p "$WORK/flash"
echo DISABLE > "$WORK/flash/zerotouch-config"
cat > "$WORK/flash/startup-config" <<'CFG'
! eAPI bootstrap for pulumi-eos integration tests.
hostname pulumi-eos-it-veos
!
username admin privilege 15 role network-admin secret 0 admin
!
no aaa root
!
management api http-commands
   no shutdown
   protocol http
   no protocol https
!
interface Management1
   ip address 10.0.2.15/24
!
ip routing
ip route 0.0.0.0/0 10.0.2.2
!
end
CFG

# Pack flash dir into a fat32 image (vEOS expects USB-attached
# config_init disk).
dd if=/dev/zero of="$WORK/flash.img" bs=1M count=64 status=none
# F32 has a min cluster count; 64 MB image is the minimum that
# mkfs.fat will accept as FAT32. Smaller images get FAT16.
mkfs.fat -F32 -n CONFIG "$WORK/flash.img" >/dev/null
mcopy -i "$WORK/flash.img" "$WORK/flash/startup-config" ::startup-config
mcopy -i "$WORK/flash.img" "$WORK/flash/zerotouch-config" ::zerotouch-config

ABOOT=/qcow/Aboot-veos-serial-8.0.2.iso
echo "[bootstrap] launching qemu (Aboot=$ABOOT)..."
exec qemu-system-x86_64 \
  -name pulumi-eos-it-veos \
  -enable-kvm \
  -cpu host \
  -smp 2 \
  -m 2048 \
  -drive file="$ABOOT",if=ide,format=raw,index=0,media=cdrom \
  -drive file="$WORK/disk.qcow2",if=ide,format=qcow2,index=1 \
  -drive file="$WORK/flash.img",if=ide,format=raw,index=2 \
  -boot d \
  -netdev user,id=mgmt,hostfwd=tcp:0.0.0.0:80-:80,hostfwd=tcp:0.0.0.0:443-:443,hostfwd=tcp:0.0.0.0:22-:22 \
  -device e1000,netdev=mgmt,mac=52:54:00:01:01:01 \
  -nographic \
  -serial mon:stdio \
  -no-reboot
