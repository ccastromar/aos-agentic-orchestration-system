# Spring Boot H2 Demo

Proyecto Maven con Spring Boot 3.2.12, Java 17, Spring Web, Spring Data JPA y H2.

## Ejecutar

```bash
mvn spring-boot:run
```

La API queda disponible en `http://localhost:8080/api/tasks` y la consola H2 en
`http://localhost:8080/h2-console`.

Credenciales H2:

- JDBC URL: `jdbc:h2:mem:tasks`
- User: `sa`
- Password: vacío

## Ejemplos

```bash
curl http://localhost:8080/api/tasks

curl -X POST http://localhost:8080/api/tasks \
  -H 'Content-Type: application/json' \
  -d '{"title":"Nueva tarea"}'
```
