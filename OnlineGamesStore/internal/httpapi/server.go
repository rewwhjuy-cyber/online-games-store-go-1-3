package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vasilenko/online-games-store/internal/auth"
	"github.com/vasilenko/online-games-store/internal/domain"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	DB                    *pgxpool.Pool
	Secret                string
	AccessTTL, RefreshTTL time.Duration
	mu                    sync.Mutex
	hits                  map[string][]time.Time
}
type ctxKey string

const userKey ctxKey = "user"

type principal struct {
	ID   int64
	Role string
}

func New(db *pgxpool.Pool, secret string, a, r time.Duration) *Server {
	return &Server{DB: db, Secret: secret, AccessTTL: a, RefreshTTL: r, hits: map[string][]time.Time{}}
}
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /health", s.health)
	m.HandleFunc("GET /api/games", s.listGames)
	m.HandleFunc("GET /api/games/{id}", s.getGame)
	m.HandleFunc("POST /api/auth/register", s.register)
	m.HandleFunc("POST /api/auth/login", s.login)
	m.HandleFunc("POST /api/auth/refresh", s.refresh)
	m.HandleFunc("POST /api/auth/logout", s.auth(s.logout))
	m.HandleFunc("GET /api/me", s.auth(s.me))
	m.HandleFunc("GET /api/cart", s.auth(s.cart))
	m.HandleFunc("POST /api/cart/items", s.auth(s.addCartItem))
	m.HandleFunc("DELETE /api/cart/items/{gameID}", s.auth(s.removeCartItem))
	m.HandleFunc("POST /api/orders", s.auth(s.createOrder))
	m.HandleFunc("GET /api/orders", s.auth(s.listOrders))
	m.HandleFunc("GET /api/orders/{id}", s.auth(s.getOrder))
	m.HandleFunc("PATCH /api/orders/{id}/status", s.auth(s.roles("ADMIN", "MANAGER")(s.changeOrderStatus)))
	m.HandleFunc("POST /api/games", s.auth(s.roles("ADMIN", "MANAGER")(s.createGame)))
	m.HandleFunc("PATCH /api/games/{id}", s.auth(s.roles("ADMIN", "MANAGER")(s.updateGame)))
	m.HandleFunc("GET /api/admin/audit", s.auth(s.roles("ADMIN")(s.audit)))
	return s.security(s.rateLimit(m))
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if e := s.DB.Ping(r.Context()); e != nil {
		writeErr(w, 503, "database unavailable")
		return
	}
	write(w, 200, map[string]any{"status": "ok", "service": "online-games-store"})
}
func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			ip = host
		}
		s.mu.Lock()
		now := time.Now()
		old := s.hits[ip][:0]
		for _, t := range s.hits[ip] {
			if now.Sub(t) < time.Minute {
				old = append(old, t)
			}
		}
		if len(old) >= 120 {
			s.hits[ip] = old
			s.mu.Unlock()
			writeErr(w, 429, "rate limit exceeded")
			return
		}
		s.hits[ip] = append(old, now)
		s.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := auth.Bearer(r.Header.Get("Authorization"))
		c, e := auth.ParseAccess(s.Secret, raw)
		if e != nil {
			writeErr(w, 401, "authentication required")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, principal{c.UserID, c.Role})
		next(w, r.WithContext(ctx))
	}
}
func (s *Server) roles(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			p := current(r)
			for _, role := range roles {
				if p.Role == role {
					next(w, r)
					return
				}
			}
			writeErr(w, 403, "forbidden")
		}
	}
}
func current(r *http.Request) principal { p, _ := r.Context().Value(userKey).(principal); return p }
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Email, Password string }
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || !strings.Contains(in.Email, "@") || len(in.Password) < 8 {
		writeErr(w, 400, "name, valid email and password (8+ chars) are required")
		return
	}
	h, e := auth.HashPassword(in.Password)
	if e != nil {
		writeErr(w, 500, "could not hash password")
		return
	}
	var id int64
	e = s.DB.QueryRow(r.Context(), `INSERT INTO users(name,email,password_hash,role) VALUES($1,lower($2),$3,'CUSTOMER') RETURNING id`, strings.TrimSpace(in.Name), in.Email, h).Scan(&id)
	if e != nil {
		if strings.Contains(e.Error(), "duplicate key") {
			writeErr(w, 409, "email already registered")
		} else {
			writeErr(w, 500, "could not create user")
		}
		return
	}
	s.issueSession(w, r, id, "CUSTOMER")
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !decode(w, r, &in) {
		return
	}
	var id int64
	var hash, role string
	e := s.DB.QueryRow(r.Context(), `SELECT id,password_hash,role FROM users WHERE email=lower($1)`, in.Email).Scan(&id, &hash, &role)
	if e != nil || !auth.CheckPassword(hash, in.Password) {
		writeErr(w, 401, "invalid email or password")
		return
	}
	s.issueSession(w, r, id, role)
}
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, id int64, role string) {
	access, e := auth.NewAccess(s.Secret, s.AccessTTL, id, role)
	if e != nil {
		writeErr(w, 500, "token error")
		return
	}
	raw, hash, e := auth.NewRefresh()
	if e != nil {
		writeErr(w, 500, "token error")
		return
	}
	_, e = s.DB.Exec(r.Context(), `INSERT INTO refresh_tokens(user_id,token_hash,expires_at) VALUES($1,$2,$3)`, id, hash, time.Now().Add(s.RefreshTTL))
	if e != nil {
		writeErr(w, 500, "session error")
		return
	}
	write(w, 200, map[string]any{"access_token": access, "refresh_token": raw, "token_type": "Bearer", "expires_in": int(s.AccessTTL.Seconds())})
}
func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decode(w, r, &in) {
		return
	}
	hash := auth.HashToken(in.RefreshToken)
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		writeErr(w, 500, "database error")
		return
	}
	defer tx.Rollback(r.Context())
	var id int64
	var expires time.Time
	e = tx.QueryRow(r.Context(), `SELECT user_id,expires_at FROM refresh_tokens WHERE token_hash=$1 AND revoked_at IS NULL FOR UPDATE`, hash).Scan(&id, &expires)
	if e != nil || time.Now().After(expires) {
		writeErr(w, 401, "invalid refresh token")
		return
	}
	if _, e = tx.Exec(r.Context(), `UPDATE refresh_tokens SET revoked_at=now() WHERE token_hash=$1`, hash); e != nil {
		writeErr(w, 500, "refresh error")
		return
	}
	var role string
	if e = tx.QueryRow(r.Context(), `SELECT role FROM users WHERE id=$1`, id).Scan(&role); e != nil {
		writeErr(w, 500, "user unavailable")
		return
	}
	raw, newHash, e := auth.NewRefresh()
	if e != nil {
		writeErr(w, 500, "token error")
		return
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO refresh_tokens(user_id,token_hash,expires_at) VALUES($1,$2,$3)`, id, newHash, time.Now().Add(s.RefreshTTL)); e != nil {
		writeErr(w, 500, "refresh error")
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeErr(w, 500, "refresh error")
		return
	}
	access, e := auth.NewAccess(s.Secret, s.AccessTTL, id, role)
	if e != nil {
		writeErr(w, 500, "token error")
		return
	}
	write(w, 200, map[string]any{"access_token": access, "refresh_token": raw, "token_type": "Bearer", "expires_in": int(s.AccessTTL.Seconds())})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decode(w, r, &in) {
		return
	}
	_, e := s.DB.Exec(r.Context(), `UPDATE refresh_tokens SET revoked_at=now() WHERE token_hash=$1 AND user_id=$2 AND revoked_at IS NULL`, auth.HashToken(in.RefreshToken), current(r).ID)
	if e != nil {
		writeErr(w, 500, "logout failed")
		return
	}
	write(w, 200, map[string]string{"message": "logged out"})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p := current(r)
	var name, email string
	if e := s.DB.QueryRow(r.Context(), `SELECT name,email FROM users WHERE id=$1`, p.ID).Scan(&name, &email); e != nil {
		writeErr(w, 404, "user not found")
		return
	}
	write(w, 200, map[string]any{"id": p.ID, "name": name, "email": email, "role": p.Role})
}
func (s *Server) listGames(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	category := r.URL.Query().Get("category")
	sortBy := r.URL.Query().Get("sortBy")
	page, limit := pagination(r)
	sortSQL := map[string]string{"price_asc": "price_cents ASC", "price_desc": "price_cents DESC", "title": "title ASC", "newest": "created_at DESC"}[sortBy]
	if sortSQL == "" {
		sortSQL = "created_at DESC"
	}
	rows, e := s.DB.Query(r.Context(), `SELECT g.id,g.title,g.description,g.platform,g.price_cents,g.stock,COALESCE(c.name,'') FROM games g LEFT JOIN categories c ON c.id=g.category_id WHERE g.is_active AND ($1='' OR g.title ILIKE '%'||$1||'%') AND ($2='' OR c.name=$2) ORDER BY `+sortSQL+` LIMIT $3 OFFSET $4`, q, category, limit, (page-1)*limit)
	if e != nil {
		writeErr(w, 500, "could not list games")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	var total int64
	for rows.Next() {
		var id int64
		var title, desc, platform, cat string
		var price int64
		var stock int
		if e = rows.Scan(&id, &title, &desc, &platform, &price, &stock, &cat); e != nil {
			writeErr(w, 500, "read error")
			return
		}
		items = append(items, map[string]any{"id": id, "title": title, "description": desc, "platform": platform, "price_cents": price, "price": money(price), "stock": stock, "category": cat})
	}
	if rows.Err() != nil {
		writeErr(w, 500, "read error")
		return
	}
	write(w, 200, map[string]any{"items": items, "page": page, "limit": limit})
}
func (s *Server) getGame(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		writeErr(w, 400, "invalid game id")
		return
	}
	var title, desc, platform, cat string
	var price int64
	var stock int
	e = s.DB.QueryRow(r.Context(), `SELECT g.title,g.description,g.platform,g.price_cents,g.stock,COALESCE(c.name,'') FROM games g LEFT JOIN categories c ON c.id=g.category_id WHERE g.id=$1 AND g.is_active`, id).Scan(&title, &desc, &platform, &price, &stock, &cat)
	if errors.Is(e, pgx.ErrNoRows) {
		writeErr(w, 404, "game not found")
		return
	}
	if e != nil {
		writeErr(w, 500, "database error")
		return
	}
	write(w, 200, map[string]any{"id": id, "title": title, "description": desc, "platform": platform, "price_cents": price, "price": money(price), "stock": stock, "category": cat})
}
func (s *Server) createGame(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title, Description, Platform string
		PriceCents                   int64 `json:"price_cents"`
		Stock                        int
		CategoryID                   *int64 `json:"category_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Title) == "" || in.PriceCents < 0 || in.Stock < 0 {
		writeErr(w, 400, "invalid game data")
		return
	}
	var id int64
	e := s.DB.QueryRow(r.Context(), `INSERT INTO games(title,description,platform,price_cents,stock,category_id) VALUES($1,$2,COALESCE(NULLIF($3,''),'PC'),$4,$5,$6) RETURNING id`, in.Title, in.Description, in.Platform, in.PriceCents, in.Stock, in.CategoryID).Scan(&id)
	if e != nil {
		writeErr(w, 400, "could not create game")
		return
	}
	s.writeAudit(r, "GAME_CREATED", "games", id, map[string]any{"title": in.Title})
	write(w, 201, map[string]any{"id": id})
}
func (s *Server) updateGame(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		writeErr(w, 400, "invalid id")
		return
	}
	var in struct {
		Title, Description, Platform string
		PriceCents                   int64 `json:"price_cents"`
		Stock                        int
		Active                       *bool `json:"is_active"`
	}
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Title) == "" || in.PriceCents < 0 || in.Stock < 0 {
		writeErr(w, 400, "invalid game data")
		return
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	tag, e := s.DB.Exec(r.Context(), `UPDATE games SET title=$1,description=$2,platform=$3,price_cents=$4,stock=$5,is_active=$6 WHERE id=$7`, in.Title, in.Description, in.Platform, in.PriceCents, in.Stock, active, id)
	if e != nil {
		writeErr(w, 500, "update failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 404, "game not found")
		return
	}
	s.writeAudit(r, "GAME_UPDATED", "games", id, map[string]any{})
	write(w, 200, map[string]string{"message": "updated"})
}
func (s *Server) cart(w http.ResponseWriter, r *http.Request) {
	p := current(r)
	rows, e := s.DB.Query(r.Context(), `SELECT g.id,g.title,g.price_cents,ci.quantity FROM cart_items ci JOIN games g ON g.id=ci.game_id WHERE ci.user_id=$1 ORDER BY g.title`, p.ID)
	if e != nil {
		if strings.Contains(e.Error(), "cart_items") {
			write(w, 200, map[string]any{"items": []any{}, "total_cents": 0})
			return
		}
		writeErr(w, 500, "cart error")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var title string
		var price int64
		var qty int
		if rows.Scan(&id, &title, &price, &qty) != nil {
			writeErr(w, 500, "cart read error")
			return
		}
		line := price * int64(qty)
		total += line
		items = append(items, map[string]any{"game_id": id, "title": title, "unit_price_cents": price, "quantity": qty, "line_total_cents": line})
	}
	if rows.Err() != nil {
		writeErr(w, 500, "cart read error")
		return
	}
	write(w, 200, map[string]any{"items": items, "total_cents": total})
}
func (s *Server) addCartItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		GameID   int64 `json:"game_id"`
		Quantity int   `json:"quantity"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.GameID <= 0 || in.Quantity <= 0 || in.Quantity > 20 {
		writeErr(w, 400, "game_id and quantity 1..20 required")
		return
	}
	p := current(r)
	tag, e := s.DB.Exec(r.Context(), `INSERT INTO cart_items(user_id,game_id,quantity) SELECT $1,id,$3 FROM games WHERE id=$2 AND is_active AND stock >= $3 ON CONFLICT(user_id,game_id) DO UPDATE SET quantity=cart_items.quantity+EXCLUDED.quantity WHERE cart_items.quantity+EXCLUDED.quantity<=20`, p.ID, in.GameID, in.Quantity)
	if e != nil {
		writeErr(w, 500, "cart update failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 409, "game unavailable or quantity limit exceeded")
		return
	}
	write(w, 200, map[string]string{"message": "cart updated"})
}
func (s *Server) removeCartItem(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("gameID"), 10, 64)
	if e != nil {
		writeErr(w, 400, "invalid game id")
		return
	}
	tag, e := s.DB.Exec(r.Context(), `DELETE FROM cart_items WHERE user_id=$1 AND game_id=$2`, current(r).ID, id)
	if e != nil {
		writeErr(w, 500, "cart update failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 404, "cart item not found")
		return
	}
	write(w, 200, map[string]string{"message": "removed"})
}
func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	p := current(r)
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		writeErr(w, 500, "database error")
		return
	}
	defer tx.Rollback(r.Context())
	rows, e := tx.Query(r.Context(), `SELECT g.id,g.title,g.price_cents,ci.quantity,g.stock FROM cart_items ci JOIN games g ON g.id=ci.game_id WHERE ci.user_id=$1 FOR UPDATE OF g`, p.ID)
	if e != nil {
		writeErr(w, 500, "cart read failed")
		return
	}
	type line struct {
		id         int64
		title      string
		price      int64
		qty, stock int
	}
	lines := []line{}
	for rows.Next() {
		var x line
		if e = rows.Scan(&x.id, &x.title, &x.price, &x.qty, &x.stock); e != nil {
			rows.Close()
			writeErr(w, 500, "cart read failed")
			return
		}
		if x.qty > x.stock {
			rows.Close()
			writeErr(w, 409, "insufficient stock")
			return
		}
		lines = append(lines, x)
	}
	rows.Close()
	if len(lines) == 0 {
		writeErr(w, 400, "cart is empty")
		return
	}
	domainLines := make([]domain.Line, 0, len(lines))
	for _, x := range lines {
		domainLines = append(domainLines, domain.Line{Quantity: x.qty, UnitPriceCents: x.price})
	}
	total, calcErr := domain.CalculateTotal(domainLines)
	if calcErr != nil {
		writeErr(w, 400, "invalid order total")
		return
	}
	var orderID int64
	e = tx.QueryRow(r.Context(), `INSERT INTO orders(user_id,status,total_cents) VALUES($1,'NEW',$2) RETURNING id`, p.ID, total).Scan(&orderID)
	if e != nil {
		writeErr(w, 500, "order creation failed")
		return
	}
	for _, x := range lines {
		if _, e = tx.Exec(r.Context(), `INSERT INTO order_items(order_id,game_id,game_title,quantity,unit_price_cents) VALUES($1,$2,$3,$4,$5)`, orderID, x.id, x.title, x.qty, x.price); e != nil {
			writeErr(w, 500, "order item failed")
			return
		}
		if _, e = tx.Exec(r.Context(), `UPDATE games SET stock=stock-$1 WHERE id=$2`, x.qty, x.id); e != nil {
			writeErr(w, 500, "stock update failed")
			return
		}
	}
	if _, e = tx.Exec(r.Context(), `DELETE FROM cart_items WHERE user_id=$1`, p.ID); e != nil {
		writeErr(w, 500, "cart clear failed")
		return
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO audit_logs(actor_id,action,entity,entity_id,details) VALUES($1,'ORDER_CREATED','orders',$2,jsonb_build_object('total_cents',$3))`, p.ID, orderID, total); e != nil {
		writeErr(w, 500, "audit failed")
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeErr(w, 500, "order commit failed")
		return
	}
	write(w, 201, map[string]any{"order_id": orderID, "total_cents": total, "status": "NEW"})
}
func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	p := current(r)
	page, limit := pagination(r)
	status := r.URL.Query().Get("status")
	var rows pgx.Rows
	var e error
	if p.Role == "ADMIN" || p.Role == "MANAGER" {
		rows, e = s.DB.Query(r.Context(), `SELECT id,user_id,status,total_cents,created_at FROM orders WHERE ($1='' OR status=$1) ORDER BY created_at DESC LIMIT $2 OFFSET $3`, status, limit, (page-1)*limit)
	} else {
		rows, e = s.DB.Query(r.Context(), `SELECT id,user_id,status,total_cents,created_at FROM orders WHERE user_id=$1 AND ($2='' OR status=$2) ORDER BY created_at DESC LIMIT $3 OFFSET $4`, p.ID, status, limit, (page-1)*limit)
	}
	if e != nil {
		writeErr(w, 500, "orders error")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, uid, total int64
		var st string
		var created time.Time
		if rows.Scan(&id, &uid, &st, &total, &created) != nil {
			writeErr(w, 500, "orders read error")
			return
		}
		items = append(items, map[string]any{"id": id, "user_id": uid, "status": st, "total_cents": total, "created_at": created})
	}
	if rows.Err() != nil {
		writeErr(w, 500, "orders read error")
		return
	}
	write(w, 200, map[string]any{"items": items, "page": page, "limit": limit})
}
func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		writeErr(w, 400, "invalid order id")
		return
	}
	p := current(r)
	var uid, total int64
	var status string
	var created time.Time
	e = s.DB.QueryRow(r.Context(), `SELECT user_id,status,total_cents,created_at FROM orders WHERE id=$1`, id).Scan(&uid, &status, &total, &created)
	if errors.Is(e, pgx.ErrNoRows) {
		writeErr(w, 404, "order not found")
		return
	}
	if e != nil {
		writeErr(w, 500, "order error")
		return
	}
	if p.Role != "ADMIN" && p.Role != "MANAGER" && uid != p.ID {
		writeErr(w, 404, "order not found")
		return
	}
	rows, e := s.DB.Query(r.Context(), `SELECT game_id,game_title,quantity,unit_price_cents FROM order_items WHERE order_id=$1`, id)
	if e != nil {
		writeErr(w, 500, "items error")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var gid int64
		var title string
		var qty int
		var price int64
		if rows.Scan(&gid, &title, &qty, &price) != nil {
			writeErr(w, 500, "items error")
			return
		}
		items = append(items, map[string]any{"game_id": gid, "title": title, "quantity": qty, "unit_price_cents": price})
	}
	write(w, 200, map[string]any{"id": id, "status": status, "total_cents": total, "created_at": created, "items": items})
}
func (s *Server) changeOrderStatus(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		writeErr(w, 400, "invalid order id")
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &in) {
		return
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		writeErr(w, 500, "database error")
		return
	}
	defer tx.Rollback(r.Context())
	var old string
	e = tx.QueryRow(r.Context(), `SELECT status FROM orders WHERE id=$1 FOR UPDATE`, id).Scan(&old)
	if errors.Is(e, pgx.ErrNoRows) {
		writeErr(w, 404, "order not found")
		return
	}
	if e != nil {
		writeErr(w, 500, "order error")
		return
	}
	if !domain.CanTransition(old, in.Status) {
		writeErr(w, 409, "invalid status transition")
		return
	}
	if _, e = tx.Exec(r.Context(), `UPDATE orders SET status=$1,updated_at=now() WHERE id=$2`, in.Status, id); e != nil {
		writeErr(w, 500, "status update failed")
		return
	}
	if in.Status == "CANCELLED" {
		if _, e = tx.Exec(r.Context(), `UPDATE games g SET stock=g.stock+oi.quantity FROM order_items oi WHERE oi.order_id=$1 AND oi.game_id=g.id`, id); e != nil {
			writeErr(w, 500, "stock restoration failed")
			return
		}
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO audit_logs(actor_id,action,entity,entity_id,details) VALUES($1,'ORDER_STATUS_CHANGED','orders',$2,jsonb_build_object('from',$3,'to',$4))`, current(r).ID, id, old, in.Status); e != nil {
		writeErr(w, 500, "audit failed")
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeErr(w, 500, "commit failed")
		return
	}
	write(w, 200, map[string]string{"status": in.Status})
}
func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	page, limit := pagination(r)
	rows, e := s.DB.Query(r.Context(), `SELECT id,actor_id,action,entity,entity_id,details,created_at FROM audit_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, (page-1)*limit)
	if e != nil {
		writeErr(w, 500, "audit error")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var actor, entityID *int64
		var action, entity string
		var details []byte
		var created time.Time
		if rows.Scan(&id, &actor, &action, &entity, &entityID, &details, &created) != nil {
			writeErr(w, 500, "audit read error")
			return
		}
		out = append(out, map[string]any{"id": id, "actor_id": actor, "action": action, "entity": entity, "entity_id": entityID, "details": jsonRaw(details), "created_at": created})
	}
	write(w, 200, map[string]any{"items": out, "page": page, "limit": limit})
}
func (s *Server) writeAudit(r *http.Request, action, entity string, id int64, details map[string]any) {
	_, _ = s.DB.Exec(r.Context(), `INSERT INTO audit_logs(actor_id,action,entity,entity_id,details) VALUES($1,$2,$3,$4,$5::jsonb)`, current(r).ID, action, entity, id, auth.Encode(details))
}
func pagination(r *http.Request) (int, int) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	l, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if p < 1 {
		p = 1
	}
	if l < 1 {
		l = 20
	}
	if l > 100 {
		l = 100
	}
	return p, l
}
func money(c int64) string { return fmt.Sprintf("%d.%02d", c/100, c%100) }
func jsonRaw(b []byte) any {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return map[string]any{}
	}
	return v
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		writeErr(w, 400, "invalid JSON body")
		return false
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		writeErr(w, 400, "request must contain one JSON value")
		return false
	}
	return true
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeErr(w http.ResponseWriter, status int, msg string) {
	write(w, status, map[string]string{"error": msg})
}
