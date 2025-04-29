# STDIO MCP Client Example

This example demonstrates how to connect to the `emcee` MCP server using the STDIO transport.

## Usage

```bash
go run main.go
```

This example:

1. Starts the `emcee` command as a subprocess with the Weather.gov OpenAPI URL
2. Connects to it using STDIO transport (stdin/stdout/stderr)
3. Initializes an MCP client over this STDIO connection
4. Gets and displays server information and capabilities
5. Lists all available tools on the server
6. Gets weather data for zip code 80027 (Superior, CO) by:
   - Calling the getPoints tool to get location data for the zip code
   - Extracting the forecast URL from the response
   - Calling the getForecast tool to get the actual weather forecast

## How it Works

The example sets up bidirectional communication with the `emcee` command:

1. The `emcee` command is started as a subprocess with the provided OpenAPI URL
2. We create pipes for stdin, stdout, and stderr to communicate with the subprocess
3. We create an MCP client using the STDIO transport, connecting to the `emcee` subprocess
4. The client communicates with `emcee` using the JSON-RPC protocol over stdin/stdout

This pattern is useful for integrating with command-line tools that support the MCP protocol over STDIO.