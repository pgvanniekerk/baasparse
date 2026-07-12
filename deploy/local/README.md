# Local deployment overrides

`values.local.yaml` holds this machine's secrets and is **gitignored**. It is not
optional: the chart refuses to render without a `secretKey`, on purpose.

## Why the key is not in the repo

`secretKey` is the AES-GCM key that encrypts stored datasource credentials (S3
secret keys, SFTP passwords). Two properties make it dangerous to hard-code as a
chart default:

- **Shipping it makes it worthless.** A default committed to the repo means every
  deployment encrypts its credentials with a key anyone can read.
- **Rotating it destroys data.** Credentials encrypted with the old key cannot be
  decrypted with a new one. There is no recovery — they must be re-entered.

The chart therefore fails loudly when it is missing rather than falling back. The
fallback would be worse than the error: with no `BAASPARSE_SECRET_KEY` the app
derives a key per pod, so a credential encrypted by one replica is unreadable by
the next — a corruption that only shows up later, under load, on the pod that did
not write it.

## Create one

    mkdir -p deploy/local
    printf 'secretKey: "%s"\n' "$(openssl rand -base64 32)" > deploy/local/values.local.yaml

## Deploy

    ./deploy/redeploy.sh              # passes -f deploy/local/values.local.yaml

In production, supply the key from a real Secret manager instead of a file.
