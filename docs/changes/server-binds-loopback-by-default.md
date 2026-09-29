---
type: breaking
---

# The server listens on `127.0.0.1` and refuses a non-loopback bind

The server used to listen on every interface. It now listens on `127.0.0.1`
unless `SUBSTRATE_BIND_ADDRESS` names another address, and it refuses to
start on any address that is not loopback, `0.0.0.0` and empty included,
unless `SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true` is set. The server speaks
plain HTTP, so the setting states that only a TLS terminator or a loopback
port mapping reaches the port. This hits a binary that other machines reach
directly or through a proxy on another host, and an image of your own built
around the binary. The published image and this repository's `compose.yaml`
set both variables. A refused boot exits with:

```
SUBSTRATE_BIND_ADDRESS is "0.0.0.0", so the server would listen on 0.0.0.0:8080, which is not loopback, and it speaks plain HTTP: passwords, TOTP codes and bearer tokens would reach the network unencrypted. ...
```

## What to do

1. Compose: with this repository's `compose.yaml`, or any compose file that
   runs `ghcr.io/geoah/substrate`, nothing. A compose file that runs an image
   of your own adds, under the substrate service's `environment:`:

   ```yaml
   SUBSTRATE_BIND_ADDRESS: 0.0.0.0
   SUBSTRATE_INSECURE_ALLOW_CLEARTEXT: "true"
   ```

2. Kubernetes: with `ghcr.io/geoah/substrate`, nothing; the image sets both.
   A container built from an image of your own adds, under its `env:`:

   ```yaml
   - name: SUBSTRATE_BIND_ADDRESS
     value: 0.0.0.0
   - name: SUBSTRATE_INSECURE_ALLOW_CLEARTEXT
     value: "true"
   ```

3. A bare binary behind a proxy on the same host: nothing; point the proxy at
   `127.0.0.1:8080`. Behind a proxy on another host, add to the service's
   environment the interface the proxy reaches and the escape, and keep every
   other peer off the port:

   ```
   SUBSTRATE_BIND_ADDRESS=192.0.2.10 SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true
   ```

4. A binary that other machines reach with no TLS terminator in front is not
   a supported deployment. Put one in front
   ([TLS and the reverse proxy](../operations.md#tls-and-the-reverse-proxy)).
