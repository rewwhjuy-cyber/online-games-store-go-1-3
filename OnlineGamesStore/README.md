# Online Games Store — Go Backend

**Студент:** Василенко Александр  
**Группа:** ИСИП-36  
**Дисциплина:** Ревьюирование программных продуктов  
**Тема:** интернет-магазин цифровых компьютерных игр  

Учебный REST API на Go и PostgreSQL. Репозиторий адаптирует технические требования лабораторных №1–3 (первоначально описанные на примере CourierService) к предметной области Online Games Store. Курьерские сущности заменены каталогом игр, корзиной, покупателями и заказами.

## Реализовано

- REST API на `net/http`, разделение HTTP-слоя, конфигурации, доменной логики и PostgreSQL.
- PostgreSQL migrations: пользователи, категории, игры, заказы, позиции заказов, refresh-токены, аудит и корзина.
- Каталог игр: поиск по названию, фильтр категории, пагинация, сортировка по белому списку.
- Регистрация и вход; пароль хранится как bcrypt-хеш; роли `ADMIN`, `MANAGER`, `CUSTOMER`.
- Access JWT (HS256, issuer/audience/expiry) и refresh-токены: в БД хранится SHA-256-хеш, ротация выполняется транзакционно.
- Корзина, серверный расчёт суммы в целых копейках/центах, оформление заказа в транзакции, блокировка строк игр `FOR UPDATE`, проверка остатков.
- Жизненный цикл заказа `NEW → PAID → PROCESSING → COMPLETED`; отмена из незавершённых статусов. При отмене остатки возвращаются.
- Проверка владельца заказа (чужой заказ для CUSTOMER возвращает 404), RBAC, audit log, rate limit 120 запросов/минуту на IP.
- Миграции выполняются автоматически при запуске.

## Требования

- Go 1.23+
- Docker Desktop (для PostgreSQL) или локальный PostgreSQL 14+
- Git

## Запуск на Windows (PowerShell)

1. Распаковать проект и открыть папку `OnlineGamesStore` в терминале.
2. Запустить базу:

```powershell
docker compose up -d
```

3. Создать локальный `.env` из примера и заменить JWT_SECRET на случайную строку длиной не менее 32 символов:

```powershell
Copy-Item .env.example .env
```

4. Установить зависимости и запустить API:

```powershell
go mod tidy
go run ./cmd/api
```

API будет доступен по адресу `http://localhost:8080`. Миграции применяются автоматически. Проверка:

```powershell
curl.exe -i http://localhost:8080/health
```

Ожидается HTTP 200 и JSON `{"status":"ok","service":"online-games-store"}`.

## Основные endpoints

| Метод | Путь | Доступ | Назначение |
|---|---|---|---|
| GET | `/health` | public | Проверка API и PostgreSQL |
| GET | `/api/games` | public | Каталог, `q`, `category`, `page`, `limit`, `sortBy` |
| GET | `/api/games/{id}` | public | Карточка игры |
| POST | `/api/auth/register` | public | Регистрация покупателя |
| POST | `/api/auth/login` | public | Вход |
| POST | `/api/auth/refresh` | public | Ротация refresh-токена |
| POST | `/api/auth/logout` | CUSTOMER+ | Отзыв refresh-токена |
| GET | `/api/me` | CUSTOMER+ | Текущий пользователь |
| GET/POST | `/api/cart`, `/api/cart/items` | CUSTOMER+ | Просмотр/добавление в корзину |
| DELETE | `/api/cart/items/{gameID}` | CUSTOMER+ | Удаление из корзины |
| POST | `/api/orders` | CUSTOMER+ | Оформление заказа |
| GET | `/api/orders` | CUSTOMER+ | История заказов; менеджер видит все |
| GET | `/api/orders/{id}` | CUSTOMER+ | Детали своего заказа |
| PATCH | `/api/orders/{id}/status` | ADMIN/MANAGER | Изменение статуса заказа |
| POST/PATCH | `/api/games`, `/api/games/{id}` | ADMIN/MANAGER | Управление каталогом |
| GET | `/api/admin/audit` | ADMIN | Журнал аудита |

JSON регистрации: `{"name":"Alex","email":"alex@example.com","password":"StrongPass123"}`. Роль при регистрации не принимается от клиента и всегда `CUSTOMER`.

JSON добавления игры в корзину: `{"game_id":1,"quantity":1}`. JSON создания заказа не принимает сумму: итог вычисляется сервером из актуальных цен каталога.

## Первичная настройка администратора

Публичная регистрация намеренно не позволяет назначить себе ADMIN/MANAGER. После первой регистрации администратора назначает владелец БД вручную:

```sql
UPDATE users SET role='ADMIN' WHERE email='your-admin@example.com';
```

## Проверки

```powershell
go test ./...
go test -race ./...
go vet ./...
```

Unit-тесты доменной логики и auth не требуют PostgreSQL. Проверка HTTP endpoints и транзакций требует работающей БД. Не заявляйте, что тесты прошли, если не запускали команды в своей среде; сохраните фактический вывод терминала для отчёта.

## GitHub

Рекомендуемое имя репозитория: `online-games-store-go`. Загружать нужно содержимое этой папки, не ZIP-архив. Не загружайте `.env`, пароли, токены, `vendor/` и бинарные файлы.

## Структура

```text
cmd/api/                 точка входа
internal/config/         переменные окружения
internal/db/             PostgreSQL pool и миграции
internal/httpapi/        REST endpoints, middleware, RBAC
internal/auth/           bcrypt, JWT, refresh token hash
internal/domain/         расчёт стоимости и правила статусов
migrations/              SQL up/down
 docs/                   архитектура, матрица ролей, тест-план
```

## Учебные ограничения

Это учебный backend, а не готовая коммерческая платёжная система. Нет интеграции с платёжным провайдером, выдачи игровых ключей и email-подтверждения. Перед реальным использованием нужны отдельный security-аудит, HTTPS/reverse proxy, централизованный rate limiting, мониторинг и полноценные интеграционные тесты с PostgreSQL.
