# Referencia OpenAPI de la API

`openapi.json` es la especificación que publica la API en `GET /api/docs/json` (Swagger), exportada de la API en ejecución. La página del proyecto la muestra con Swagger UI en `api.html`, solo como referencia de lectura.

Para actualizarla, levanta el sistema y vuelve a exportarla:

```bash
curl -sk https://localhost:8080/api/docs/json | python3 -m json.tool > docs/api/openapi.json
```

Haz el cambio por pull request junto con el cambio de rutas que lo motivó.
