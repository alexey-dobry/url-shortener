# URL Shortener — практическая работа №8

Развитие проекта из ПР №7. Добавлены:

- **Подкоманды** — `server`, `migrate`, `create-admin`, `clear-cache`, `healthcheck`
- **Версионированные миграции** с таблицей `schema_migrations` (идемпотентные)
- **Миграции вынесены из запуска** — отдельный одноразовый процесс
- **CI deploy** — шаг `migrate` перед деплоем, если упал → деплой стоп

## Структура

```
.
├── cmd/app/main.go         # подкоманды: server, migrate, create-admin, clear-cache
├── internal/
│   ├── migrate/            # NEW: версионированные миграции
│   ├── config/
│   ├── logger/
│   ├── middleware/
│   ├── service/
│   ├── handlers/
│   ├── sessions/
│   └── storage/            # + ExecRaw, QueryVersions
├── deploy/
│   ├── nginx.conf
│   └── *.env
├── .github/workflows/
│   ├── build.yml           # сборка + push
│   └── deploy.yml          # migrate → deploy
├── Dockerfile              # ENTRYPOINT [app] CMD [server]
└── docker-compose.yml
```
