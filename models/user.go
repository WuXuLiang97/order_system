package models

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 用户角色常量
const (
	RoleAdmin = "admin" // 管理员：拥有全部权限
	RoleUser  = "user"  // 普通用户：仅查看权限
)

var (
	ErrUserNotFound   = errors.New("user not found")
	ErrUsernameExists = errors.New("username already exists")
	ErrInvalidRole    = errors.New("invalid role")
)

// User 用户模型
type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"display_name"`
	Role         string    `json:"role"`
	Permissions  []string  `json:"permissions"`
	CreatedAt    time.Time `json:"created_at"`
}

// IsAdmin 是否为管理员
func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

// IsValidRole 判断角色是否合法
func IsValidRole(role string) bool {
	return role == RoleAdmin || role == RoleUser
}

// HashPassword 使用 PBKDF2-SHA256 生成密码哈希
// 存储格式：pbkdf2$迭代次数$盐(hex)$哈希(hex)
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	iterations := 210000
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2$%d$%s$%s", iterations, hex.EncodeToString(salt), hex.EncodeToString(key)), nil
}

// CheckPassword 校验密码与存储的哈希是否匹配
func CheckPassword(password, storedHash string) bool {
	parts := strings.Split(storedHash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(parts[3])
	if err != nil {
		return false
	}
	computed, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(computed, expected) == 1
}

// GetAllUsers 获取所有用户
func GetAllUsers() ([]User, error) {
	rows, err := DB.Query("SELECT id, username, password_hash, display_name, role, created_at FROM users ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// GetUserByUsername 根据用户名获取用户
func GetUserByUsername(username string) (*User, error) {
	var u User
	err := DB.QueryRow(
		"SELECT id, username, password_hash, display_name, role, created_at FROM users WHERE username = ?",
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByID 根据ID获取用户
func GetUserByID(id int) (*User, error) {
	var u User
	err := DB.QueryRow(
		"SELECT id, username, password_hash, display_name, role, created_at FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateUser 创建用户
func CreateUser(username, passwordHash, displayName, role string) (int64, error) {
	result, err := DB.Exec(
		"INSERT INTO users (username, password_hash, display_name, role) VALUES (?, ?, ?, ?)",
		username, passwordHash, displayName, role,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// CreateUserWithPermissions 创建用户并（对普通用户）写入初始权限，同一事务完成。
func CreateUserWithPermissions(username, passwordHash, displayName, role string, perms []string) (int64, error) {
	tx, err := DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(
		"INSERT INTO users (username, password_hash, display_name, role) VALUES (?, ?, ?, ?)",
		username, passwordHash, displayName, role,
	)
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()
	for _, p := range perms {
		if _, err := tx.Exec("INSERT INTO user_permissions (user_id, permission) VALUES (?, ?)", id, p); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateUserDisplayName 更新用户显示名称
func UpdateUserDisplayName(id int, displayName string) error {
	_, err := DB.Exec("UPDATE users SET display_name = ? WHERE id = ?", displayName, id)
	return err
}

// UpdateUserPassword 更新用户密码
func UpdateUserPassword(id int, passwordHash string) error {
	_, err := DB.Exec("UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, id)
	return err
}

// DeleteUser 删除用户
func DeleteUser(id int) error {
	_, err := DB.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

// CountUsers 统计用户数量
func CountUsers() (int, error) {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

// EnsureUsersTable 确保 users 表存在，并在没有任何用户时写入默认账户
// 默认账户：admin/admin123（管理员）、viewer/viewer123（普通用户，仅查看）
func EnsureUsersTable() error {
	_, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS users (
            id            INT AUTO_INCREMENT PRIMARY KEY COMMENT '用户ID',
            username      VARCHAR(50)  NOT NULL UNIQUE COMMENT '登录用户名',
            password_hash VARCHAR(255) NOT NULL COMMENT '密码哈希(PBKDF2)',
            display_name  VARCHAR(100) NOT NULL DEFAULT '' COMMENT '显示名称',
            role          VARCHAR(20)  NOT NULL DEFAULT 'user' COMMENT '角色: admin-管理员, user-普通用户(只读)',
            created_at    DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='系统用户表'
    `)
	if err != nil {
		return err
	}

	count, err := CountUsers()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	adminHash, err := HashPassword("admin123")
	if err != nil {
		return err
	}
	viewerHash, err := HashPassword("viewer123")
	if err != nil {
		return err
	}

	if _, err := CreateUser("admin", adminHash, "管理员", RoleAdmin); err != nil {
		return err
	}
	if _, err := CreateUser("viewer", viewerHash, "普通用户", RoleUser); err != nil {
		return err
	}
	return nil
}

// EnsureUserPermissions 确保 user_permissions 表存在，并把老版普通用户默认的模块查看权限一次性写入。
func EnsureUserPermissions() error {
	if _, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS user_permissions (
            user_id    INT         NOT NULL COMMENT '用户ID',
            permission VARCHAR(50) NOT NULL COMMENT '权限标识',
            PRIMARY KEY (user_id, permission),
            CONSTRAINT fk_user_permissions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户权限表'
    `); err != nil {
		return err
	}
	if err := seedDefaultViewPermissions(); err != nil {
		return err
	}
	return migrateOrderPermissionsV2()
}

// seedDefaultViewPermissions 一次性迁移：给升级前已存在的普通用户补上原默认的模块查看权限。
// 使用 schema_migrations 防止重复执行（管理员后续手动收窄的授权不会被再次覆盖）。
func seedDefaultViewPermissions() error {
	if _, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS schema_migrations (
            migration_name VARCHAR(100) PRIMARY KEY,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
    `); err != nil {
		return err
	}

	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE migration_name = 'user_permissions_default_view'").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	defaults := []string{
		"order:view_all", "product:view", "material:view", "purchase:view", "customer:view",
	}
	rows, err := DB.Query("SELECT id FROM users WHERE role <> 'admin'")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var uid int
		if err := rows.Scan(&uid); err != nil {
			return err
		}
		for _, p := range defaults {
			if _, err := DB.Exec("INSERT IGNORE INTO user_permissions (user_id, permission) VALUES (?, ?)", uid, p); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := DB.Exec("INSERT IGNORE INTO schema_migrations (migration_name) VALUES ('user_permissions_default_view')"); err != nil {
		return err
	}
	return nil
}

// GetUserPermissions 获取用户已授权的权限标识列表。
func GetUserPermissions(userID int) ([]string, error) {
	rows, err := DB.Query("SELECT permission FROM user_permissions WHERE user_id = ? ORDER BY permission", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var perms []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// SetUserPermissions 全量覆盖保存某用户的授权。
func SetUserPermissions(userID int, perms []string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM user_permissions WHERE user_id = ?", userID); err != nil {
		return err
	}
	for _, p := range perms {
		if _, err := tx.Exec("INSERT INTO user_permissions (user_id, permission) VALUES (?, ?)", userID, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// migrateOrderPermissionsV2 将旧版订单权限迁移为新版“范围式”权限：
//
//	order:view -> order:view_all；order:create/update/delete（任一）-> order:edit_all。
func migrateOrderPermissionsV2() error {
	if _, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS schema_migrations (
            migration_name VARCHAR(100) PRIMARY KEY,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
    `); err != nil {
		return err
	}
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE migration_name = 'order_permissions_v2'").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	rows, err := DB.Query("SELECT DISTINCT user_id FROM user_permissions WHERE permission IN ('order:create','order:update','order:delete')")
	if err != nil {
		return err
	}
	var editUserIDs []int
	for rows.Next() {
		var uid int
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return err
		}
		editUserIDs = append(editUserIDs, uid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := DB.Exec("DELETE FROM user_permissions WHERE permission IN ('order:create','order:update','order:delete')"); err != nil {
		return err
	}
	if _, err := DB.Exec("UPDATE user_permissions SET permission = 'order:view_all' WHERE permission = 'order:view'"); err != nil {
		return err
	}
	for _, uid := range editUserIDs {
		for _, p := range []string{"order:edit_all", "order:view_all"} {
			if _, err := DB.Exec("INSERT IGNORE INTO user_permissions (user_id, permission) VALUES (?, ?)", uid, p); err != nil {
				return err
			}
		}
	}
	if _, err := DB.Exec("INSERT IGNORE INTO schema_migrations (migration_name) VALUES ('order_permissions_v2')"); err != nil {
		return err
	}
	return nil
}
