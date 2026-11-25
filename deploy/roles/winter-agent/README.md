# role · winter_agent

Pre-installs and removes the Agent on managed nodes (ADR-35). Used by the `agent-scale-out` and `agent-scale-in` stages; it is deliberately **not** part of the management-plane `deploy` stage.

## Identity and certificates

- `node_id` defaults to the inventory hostname and must match the certificate `CN` and `SPIFFE URI:spiffe://winter/node/<node_id>` (communication protocol v1).
- `local_csr` (default): the private key is generated **on the node** with `openssl` and never leaves it; a CSR with the node identity is staged at `{{ winter_agent.cert.dir }}/{{ winter_agent.cert.csr }}`. The service does not start until a certificate is issued.
- `preissued`: copies a key/cert pair from the control machine (offline/test path).
- The CA that signs Agent certificates is copied from `winter_agent.cert.ca_source` on the control machine. It is a required prerequisite — Ansible fails explicitly rather than silently skipping mTLS.

## Prerequisites (must exist before scale-out)

1. `winter-agent` binary published at `{{ platform.repo_base_url }}/bin/winter-agent` (produced by the Go Agent work).
2. CA certificate available on the control machine and configured as `winter_agent_overrides.cert.ca_source`.
3. Nginx `grpc_pass` for the Agent service on the documented outbound port 443 (the Agent connects out; no inbound ports are opened on the node).

If any of these is missing, the role fails with an actionable message — it never pretends the node is managed.

## Sudo whitelist

The service account `winter-agent` is non-root. Privileged actions go through an explicit sudoers file with absolute paths only; `ALL` and interpreters are forbidden (ADR-19). The default rules are read-only `ceph` commands — extend `winter_agent_overrides.sudo.rules` for `managed` capability after review.

## Scale-in safety

- Platform data is **preserved** by default.
- `scale_in.purge_data: true` requires an explicit non-empty `scale_in.data_dirs` list; otherwise the task fails rather than silently doing nothing.
- `scale_in.remove_user` is opt-in (removing the account can break UID-based ownership elsewhere).
- Certificate revocation is a Server enrollment-API concern; `scale_in.revoke_cert` only reports intent until that API exists.

## Idempotency

- Private key and CSR use `creates:` so they are generated once.
- Binary installation uses the same `cp` + same-directory `mv` pattern as the Server role (avoids `ETXTBSY`, keeps `get_url` idempotent).
- Scale-in uses `state: absent` throughout and is safe to rerun.
