# Encrypting cached tokens at rest

By default, M365Bridge stores the AES key for cached refresh tokens in the clear at `data/tokens/encryption.key`. Setting a master passphrase wraps that key under a passphrase-derived key instead, storing only the wrapped result at `data/tokens/encryption.key.enc`.

`docker-compose.secrets.yml` is an optional Compose override that enables this. It declares an env-sourced [Compose secret](https://docs.docker.com/compose/how-tos/use-secrets/): Compose reads the passphrase from `M365_MASTER_PASSPHRASE_VALUE` in the host shell and mounts it as a file at `/run/secrets/m365_master_passphrase` inside the container, so the value itself never appears in `environment:`, `docker inspect`, or `docker compose config` output.

It also moves the pepper (`data/pepper/pepper.key`, used to derive the key that wraps the DEK) off the `./data` bind mount and into a Docker-managed named volume. Someone with access to the host's `./data` directory alone — a backup, a misconfigured share, a stolen disk — gets the wrapped key file but not the pepper needed to unwrap it; the pepper exists only inside Docker's volume storage.

The `scripts/with-passphrase.sh` / `.ps1` wrapper scripts source a passphrase from the OS keychain (macOS Keychain, Linux Secret Service, Windows DPAPI) and set `M365_MASTER_PASSPHRASE_VALUE` for one command only, so it never lands in `.env` or a persistent shell variable:

```bash
scripts/with-passphrase.sh docker compose -f docker-compose.yml -f docker-compose.secrets.yml up -d
```

```powershell
scripts\with-passphrase.ps1 docker compose -f docker-compose.yml -f docker-compose.secrets.yml up -d
```

Layering in `docker-compose.secrets.yml` requires `M365_MASTER_PASSPHRASE_VALUE` to be set in the environment (the wrapper scripts do this); Compose refuses to start the container otherwise, since it can't populate the secret. Without the override file, M365Bridge logs a warning and falls back to the plaintext key. If `data/tokens/encryption.key` already exists in plaintext from an earlier run, the first run with a passphrase configured migrates it automatically: the existing key is wrapped, the plaintext file is removed, and previously cached tokens keep decrypting correctly.

`docker run` has no equivalent to Compose's env-sourced secrets. To encrypt cached tokens at rest under `docker run`, mount a passphrase file yourself and set `-e M365_MASTER_PASSPHRASE_FILE=<path inside the container>`.
