# role: realserver

Configures a node as an LVS/DR real server (ADR-16) for the Keepalived VIP: the VIP is bound to
loopback and ARP suppression is enabled, so the Director can deliver client frames while this node
answers directly with the VIP as source address. Applied by `./deploy.sh app-ha` to
`[nginx]` when `ha.lvs.enabled: true`.

## What it changes

| Path | Purpose |
| --- | --- |
| `/etc/sysctl.d/99-winter-lvs.conf` | `arp_ignore` / `arp_announce` suppression |
| `/usr/local/bin/winter-lvs-vip.sh` | Idempotent `add` / `del` helper for the loopback VIP |
| `winter-lvs-vip.service` | Applies the VIP at boot (`Type=oneshot`, `RemainAfterExit`) |

## Caveats

- The Director and all real servers must share an L2 network — DR does not route client frames.
- Services must listen on `0.0.0.0`, not on the VIP explicitly, so DR-delivered packets are accepted.
  The stock Nginx role already listens on all addresses.
- This role never removes the VIP or sysctl settings; decommissioning is an out-of-band operation.
