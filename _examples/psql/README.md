## Postgres MCP example

Start the Postgres instance with:

```sh
docker compose -f docker-compose.yml up
```

Start the server via:

```sh
go run server/main.go
```

The server will ensure the database is setup and has some seed data.


Then you can run the client:

```sh
go run client/main.go http://localhost:8081
```
