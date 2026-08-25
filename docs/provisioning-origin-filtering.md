# Provisioning Origin Filtering

`M365_PROVISION_ORIGINS` optionally limits which browser extension origins may call the session provisioning endpoint. It is a browser-facing defense in depth, not the endpoint's authorization mechanism. Every successful request still requires a payload authenticated and encrypted with `M365_PROVISION_SECRET` or `M365_PROVISION_SECRET_FILE`.

## Configuration

When `M365_PROVISION_ORIGINS` is unset or empty, browser requests from any origin are allowed by CORS:

```dotenv
M365_PROVISION_ORIGINS=
```

Use a comma-separated list to restrict requests to exact extension origins:

```dotenv
M365_PROVISION_ORIGINS=chrome-extension://<chrome-extension-id>,chrome-extension://<edge-extension-id>,moz-extension://<firefox-extension-id>
```

Chrome and Edge assign extension IDs independently, even when they load the same package. Include each browser's exact origin when using both.

The special value `*` explicitly allows every browser origin:

```dotenv
M365_PROVISION_ORIGINS=*
```

General wildcard patterns such as `chrome-extension://abc*` and `moz-extension://*` are not supported. Configure exact extension origins or use the standalone `*` value.

For `docker run`, pass the setting with `-e` when exact filtering is wanted:

```bash
-e M365_PROVISION_ORIGINS=chrome-extension://<extension-id>
```

## Security Model

An origin allowlist prevents unrelated websites and extensions from calling the endpoint through normal browser cross-origin requests. It can reduce accidental exposure and unsolicited browser-originated traffic.

An origin is not a credential. Non-browser clients can choose their own `Origin` header, so the allowlist cannot establish the caller's identity and does not protect a compromised provisioning secret. The secret-authenticated AES-GCM envelope remains the authorization boundary. Timestamp and request-ID validation additionally reject stale and replayed payloads.

For the strongest browser-facing configuration, set exact extension origins. Leave the option unset, or use `*`, when the operational convenience is worth removing that additional browser restriction.
