Got the full guide. Here it is, condensed and adapted to your setup (add-on on the HA Pi instead of a cloud server). Original: Nord Projects flashing doc. ⚠️ Reminder: ~1% chance of bricking the bridge — do it when you have time to troubleshoot.

What you need
-

- USB–TTL serial adapter (3.3V!) + jumper wires
- Philips screwdriver (open the bridge), paperclip (printer reset)
- Your Mac with a USB port
- The bridge connected to your router via ethernet (Step 4 needs it)

Part 1–2: Hook up serial
-

1. Unscrew the bridge's back panel
2. Connect the serial adapter: GND → square pin, others per the photos in the doc (tx/rx)
3. Plug the adapter into your Mac, then:
   ls /dev/cu.*                      # find the adapter, e.g. /dev/cu.SLAB_USBtoUART
   screen /dev/cu.SLAB_USBtoUART 115200 8N1
   (If nothing shows, install the SiLabs CP210x driver first.)

Part 3: Root via U-Boot
-

1. Plug in the bridge's power; watch the serial console
2. When Hit any key to stop autoboot appears, hit a key fast (miss it → pull power, retry)
3. At the U-Boot prompt:
   editenv bootargs        → add ' single' to the end, return
   boot
4. In the single-user shell:
   mount -a
   mount -o remount,rw /
   passwd                  → set e.g. mothership9d
   reboot

Part 4: Enable SSH
-

1. Bridge now needs ethernet to your router. Log in on serial (root / your password)
2. mount -o remount,rw /
   echo "PermitRootLogin yes" >> /etc/sshd_config
3. Create the init script (paste exactly as in the doc's §4.5–4.7 — the cat > /etc/init.d/S48sshd <<'EOF' … EOF block, it's a standard dropbear init script), then:
   chmod 775 /etc/init.d/S48sshd
   reboot
4. After reboot, on serial: mount -o remount,rw / then /etc/init.d/S48sshd start
5. ifconfig → note eth0 inet addr (e.g. 192.168.1.51)
6. From your Mac (modern macOS needs the legacy-algorithm flags):
   ssh -oKexAlgorithms=+diffie-hellman-group1-sha1 -oHostKeyAlgorithms=+ssh-dss root@192.168.1.51
   Part 5: Flash the new firmware
   Get the berg-bridge-dump files locally, then from that folder:
   scp -O -oKexAlgorithms=+diffie-hellman-group1-sha1 -oHostKeyAlgorithms=+ssh-dss \
   usr/bin/oneshot_bergcloud_bridge.sh root@192.168.1.51:/usr/bin/oneshot_bergcloud_bridge.sh

ssh -O -oKexAlgorithms=+diffie-hellman-group1-sha1 -oHostKeyAlgorithms=+ssh-dss \
root@192.168.1.51 "mv /usr/local/bergcloud-bridge /usr/local/bergcloud-bridge-backup"

scp -O -oKexAlgorithms=+diffie-hellman-group1-sha1 -oHostKeyAlgorithms=+ssh-dss -rp \
usr/local/bergcloud-bridge root@192.168.1.51:/usr/local/bergcloud-bridge
Then reboot on the bridge. This is the point of no return — from here it runs the hacked firmware.

Part 6: Point it at YOUR server (adapted!)
-

1. Browse to http://192.168.1.51:81/, log in berg / hereandthere
2. In the configuration box, instead of the Nord URL, enter your add-on:
   http://<ha-pi-LAN-IP>:8080/
   (the add-on listens on 0.0.0.0:8080 on the host, so the bridge reaches it directly over the LAN — no Tailscale involved for this hop)
3. Press the button; the bridge reboots (slow — be patient). Refresh the config page to confirm the URL stuck
   Part 7: Pair the printer (with your dummy-claim-code plan)
1. Paperclip-reset the printer (light off), reassemble, power on
2. When the printer's light goes solid (bridge found), press the top button → real claim code prints
3. Put the real claim code into the add-on config, restart the add-on
4. Paperclip-reset the printer again → this time pairing completes