# chaos-dl

Download and query ProjectDiscovery's Chaos subdomain dataset.

## Installation
```console
go install github.com/aldenpartridge/chaos-dl/cmd/chaos-dl@latest
```

## Usage

```
chaos-dl -r              # fetch/update index.json
chaos-dl -l              # list available programs
chaos-dl -d <name|all>   # download program(s)
chaos-dl -q <domain>     # query for a domain
```

## Options

```
-w int    concurrent workers (default: 2x CPU cores)
```

## Examples

```bash
# Download all programs
chaos-dl -r
chaos-dl -d all -w 32

# Download single program
chaos-dl -d uber

# Query and pipe to other tools
chaos-dl -q uber.com
subdomain1.uber.com
subdomain2.uber.com
subdomain3.uber.com
...


chaos-dl -q shopify.com | httpx
```
