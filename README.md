# hydrovalley
Cloud-native distributed incremental game about the 3Rs of water. Go real-time hub, Rust world simulator, TypeScript API with a React + Phaser client, Python persistence. gRPC, Docker and CI/CD. UCAB Distributed Systems project.

## Cómo se levanta

Plan B: todo el sistema en una sola máquina con Docker Compose.

```
cp deploy/.env.example deploy/.env      # y cambiar POSTGRES_PASSWORD
docker compose -f deploy/compose.yml up --build
```

Luego, abrir http://localhost:8080. La prueba de humo de punta a punta (una gota por nginx hasta Rust y una instantánea en PostgreSQL) se ejecuta con `tests/e2e/run.sh`. Más detalles en `deploy/README.md`.
