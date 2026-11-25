CREATE TABLE users (
 id BIGSERIAL PRIMARY KEY,
 name VARCHAR(120) NOT NULL CHECK (length(trim(name)) > 0),
 email VARCHAR(255) NOT NULL UNIQUE,
 password_hash TEXT NOT NULL,
 role VARCHAR(20) NOT NULL DEFAULT 'CUSTOMER' CHECK (role IN ('ADMIN','MANAGER','CUSTOMER')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE categories (
 id BIGSERIAL PRIMARY KEY,
 name VARCHAR(80) NOT NULL UNIQUE
);
CREATE TABLE games (
 id BIGSERIAL PRIMARY KEY,
 category_id BIGINT REFERENCES categories(id) ON DELETE SET NULL,
 title VARCHAR(180) NOT NULL,
 description TEXT NOT NULL DEFAULT '',
 platform VARCHAR(40) NOT NULL DEFAULT 'PC',
 price_cents BIGINT NOT NULL CHECK (price_cents >= 0),
 stock INTEGER NOT NULL DEFAULT 0 CHECK (stock >= 0),
 is_active BOOLEAN NOT NULL DEFAULT TRUE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX games_title_idx ON games USING gin (to_tsvector('simple', title));
CREATE TABLE orders (
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id),
 status VARCHAR(20) NOT NULL DEFAULT 'NEW' CHECK (status IN ('NEW','PAID','PROCESSING','COMPLETED','CANCELLED')),
 total_cents BIGINT NOT NULL DEFAULT 0 CHECK (total_cents >= 0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX orders_user_created_idx ON orders(user_id, created_at DESC);
CREATE TABLE order_items (
 id BIGSERIAL PRIMARY KEY,
 order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
 game_id BIGINT NOT NULL REFERENCES games(id),
 game_title VARCHAR(180) NOT NULL,
 quantity INTEGER NOT NULL CHECK (quantity > 0),
 unit_price_cents BIGINT NOT NULL CHECK (unit_price_cents >= 0),
 UNIQUE(order_id, game_id)
);
CREATE TABLE refresh_tokens (
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 token_hash CHAR(64) NOT NULL UNIQUE,
 expires_at TIMESTAMPTZ NOT NULL,
 revoked_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE audit_logs (
 id BIGSERIAL PRIMARY KEY,
 actor_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
 action VARCHAR(80) NOT NULL,
 entity VARCHAR(80) NOT NULL,
 entity_id BIGINT,
 details JSONB NOT NULL DEFAULT '{}'::jsonb,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_created_idx ON audit_logs(created_at DESC);

INSERT INTO categories(name) VALUES
 ('Action'),('Adventure'),('RPG'),('Strategy'),('Simulation'),('Indie')
ON CONFLICT(name) DO NOTHING;
INSERT INTO games(category_id,title,description,platform,price_cents,stock)
SELECT c.id, v.title, v.description, 'PC', v.price, v.stock
FROM (VALUES
 ('Action','Neon Horizon','Футуристический экшен в открытом мире',2499,25),
 ('Adventure','Forgotten Kingdom','Приключение с исследованием древнего мира',1899,18),
 ('RPG','Legends of Aether','Ролевая игра с развитием персонажа',3299,14),
 ('Strategy','Empire Tactics','Тактическая стратегия с пошаговыми боями',1599,20),
 ('Simulation','City Architect','Симулятор строительства современного города',2199,12),
 ('Indie','Little Planet','Независимая игра о путешествии по планетам',999,30),
 ('Action','Shadow Protocol','Шпионский боевик с заданиями',2799,10),
 ('RPG','Dragon Path','Фэнтезийная RPG с квестами',2999,16),
 ('Adventure','Ocean Mystery','Исследование затерянного подводного мира',1399,22),
 ('Strategy','Frontline Command','Стратегия управления армией',1799,11)
) AS v(category,title,description,price,stock)
JOIN categories c ON c.name=v.category
WHERE NOT EXISTS (SELECT 1 FROM games g WHERE g.title=v.title);
