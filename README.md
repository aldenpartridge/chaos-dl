# chaos-dl

Download and query ProjectDiscovery's Chaos subdomain dataset.

## Usage

```
chaos-dl -refresh              # fetch/update index.json
chaos-dl -list                 # list available programs
chaos-dl -dl <name|all>  # download program(s)
chaos-dl -q <domain>       # query for a domain
```

## Options

```
-w int    concurrent workers (default: 2x CPU cores)
```

## Examples

```bash
# Download all programs
chaos-dl -refresh
chaos-dl -dl all -w 32

# Download single program
chaos-dl -dl uber

# Query and pipe to other tools
chaos-dl -q uber.com
subdomain1.uber.com
subdomain2.uber.com
subdomain3.uber.com
...


chaos-dl -q shopify.com | httpx
```
