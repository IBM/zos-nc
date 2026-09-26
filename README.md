# zos-nc — Simplified netcat (TCP only)

A minimal `nc` clone that connects `stdin`/`stdout` to a TCP connection. Built for z/OS and other systems where full netcat isn’t available.

Supports listen mode, connect mode, verbose logging, and outbound proxying via SOCKS5 or HTTP CONNECT.

## Requirements

* Go 1.25 or later

## Build

```sh
go build -o nc
# or
make
```

Install:

```sh
make install PREFIX=/usr/local
```

## Usage

```text
nc [-v] [-l port] | [hostname port]
   [-x proxy-server:port|url] [-X 5|connect] [-u userid] [-p password]
```

| Flag | Description |
|------|-------------|
| `-l port` | Listen mode: `:n` or `b:n` where `n` is port (1-65535), `b` is bind interface. Defaults to `:n`. |
| `-x` | Proxy host (`proxy:port` or URL, depending on `-X`). |
| `-X` | Proxy protocol: `5` (SOCKS5) or `connect` (HTTP CONNECT). |
| `-u` / `-p` | Proxy userid / password (SOCKS5). |
| `-v` | Verbose logging. |

Listen mode takes no positional args. Connect mode requires `hostname port`.

### Examples

Listen on port 9899:

```sh
nc -l 9899
```

Connect to `localhost:6767`:

```sh
nc localhost 6767
```

Via SOCKS5 proxy:

```sh
nc -X 5 -x proxy:1080 -u user -p pass target-host 80
```

Via HTTP CONNECT proxy:

```sh
nc -X connect -x http://proxy:8080 target-host 80
```

### z/OS file-transfer examples

Transfer a directory from zos-A (server) to zos-B (client):

```sh
# On zos-A
/bin/pax -w -z -x pax mysource/ | nc -l 4321

# On zos-B
nc zos-A 4321 | /bin/pax -v -ppx -r
```

PC to zos-A with EBCDIC conversion:

```sh
# On zos-A
nc -l 4321 | /bin/iconv -f 1047 -t 819 | /bin/pax -v -ppx -r

# On PC
tar -cvf - mydir | nc zos-A 4321
```

With encryption:

```sh
# On zos-A
/bin/pax -w -z -x pax mysource/ | gpg -c --batch --passphrase 123456 -o - 2>/dev/null | nc -l 4321

# On zos-B
nc zos-A 4321 | gpg -d --batch --passphrase 123456 2>/dev/null | /bin/pax -v -ppx -r
```

## Limitations

* TCP only, single connection per invocation (listen accepts once then exits).
* No UDP, port scanning, or keep-listen (`-k`).

## License

See [LICENSE](LICENSE).
