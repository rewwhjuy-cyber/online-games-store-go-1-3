# Архитектура Online Games Store

## Поток запроса

`HTTP request → middleware (security/rate limit/auth/RBAC) → handler → domain rules → PostgreSQL transaction → JSON response`

## Сущности и связи

- `users 1:N orders`
- `categories 1:N games`
- `orders 1:N order_items`
- `games 1:N order_items`
- `users 1:N refresh_tokens`
- `users 1:N audit_logs`
- `users M:N games` через `cart_items` (в корзине хранится количество)

## Решения

- Цена хранится в `BIGINT` минимальных денежных единицах; `float64` не используется.
- Итог заказа рассчитывается на сервере по сохранённым ценам игр в момент оформления.
- Оформление заказа: транзакция, `SELECT ... FOR UPDATE` для строк игр, проверка stock, вставка заказа/позиций, уменьшение остатков, очистка корзины и запись аудита до commit.
- Изменение статуса блокирует заказ `FOR UPDATE`, проверяет конечный автомат, записывает аудит в той же транзакции.
- При отмене незавершённого заказа остатки игр возвращаются в той же транзакции.
- SQL-параметры передаются отдельно; `sortBy` выбирается только из фиксированного whitelist.
