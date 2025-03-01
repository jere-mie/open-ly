package main

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/template/django/v3"

	"github.com/joho/godotenv"
)

//go:embed templates
var TemplateAssets embed.FS

//go:embed static/*
var StaticAssets embed.FS

//go:embed version.txt
var Version string

// Global database mutex for synchronizing write operations
var dbMutex sync.Mutex

type Link struct {
	ID        int
	ShortID   string
	LongURL   string
	CreatedAt time.Time
}

func main() {
	log.Println("Starting the application...")

	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using default values")
	}

	// Connect to database "openly.db"
	db, err := sql.Open("sqlite", "openly.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Enable Write-Ahead Logging (WAL) mode for better concurrency
	_, err = db.Exec("PRAGMA journal_mode=WAL;")
	if err != nil {
		log.Fatal(err)
	}

	// Set database connection pooling limits
	db.SetMaxOpenConns(1) // SQLite supports only one writer at a time
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Minute * 5)

	// Create tables if they don't exist
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS links (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		short_id TEXT NOT NULL UNIQUE,
		long_url TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL UNIQUE,
		expiry_time DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`)
	if err != nil {
		log.Fatal(err)
	}

	// Embed /templates directory into binary
	engine := django.NewPathForwardingFileSystem(http.FS(TemplateAssets), "/templates", ".html")
	app := fiber.New(fiber.Config{
		Views:             engine,
		PassLocalsToViews: true,
		ErrorHandler: func(ctx *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			var e *fiber.Error
			if errors.As(err, &e) {
				code = e.Code
			}

			if code == 404 || code == 500 {
				err = ctx.Render(fmt.Sprintf("%d", code), fiber.Map{})
			}

			if err != nil {
				return ctx.Status(fiber.StatusInternalServerError).SendString("Internal Server Error")
			}
			return nil
		},
	})

	app.Use(logger.New())
	app.Use(currentUserMiddleware(db))

	// Embed /static assets into binary
	app.Use("/static", filesystem.New(filesystem.Config{
		Root:       http.FS(StaticAssets),
		PathPrefix: "static",
		Browse:     false,
	}))

	PORT := GetEnv("PORT", "3000")

	app.Get("/", func(c *fiber.Ctx) error {
		return c.Render("index", fiber.Map{})
	})

	app.Get("/loginadmin", func(c *fiber.Ctx) error {
		return c.Render("login", fiber.Map{})
	})

	app.Get("/admin", func(c *fiber.Ctx) error {
		var currentUser = c.Locals("current_user")
		if currentUser == nil {
			log.Println("Admin login required")
			return c.Render("404", fiber.Map{})
		}

		rows, err := db.Query("SELECT id, short_id, long_url, created_at FROM links")
		if err != nil {
			log.Println(err)
			return c.Render("500", fiber.Map{})
		}
		defer rows.Close()

		var links []Link
		for rows.Next() {
			var link Link
			err := rows.Scan(&link.ID, &link.ShortID, &link.LongURL, &link.CreatedAt)
			if err != nil {
				log.Println(err)
				return c.Render("500", fiber.Map{})
			}
			links = append(links, link)
		}

		return c.Render("admin", fiber.Map{
			"links": links,
		})
	})

	app.Get("/new", func(c *fiber.Ctx) error {
		var currentUser = c.Locals("current_user")
		if currentUser == nil {
			return c.Render("404", fiber.Map{})
		}
		return c.Render("new", fiber.Map{})
	})

	app.Get("/logout", func(c *fiber.Ctx) error {
		sessionID := c.Cookies("session_id")
		if sessionID != "" {
			dbMutex.Lock()
			defer dbMutex.Unlock()

			stmt, err := db.Prepare("DELETE FROM sessions WHERE session_id = ?")
			if err != nil {
				log.Println(err)
				return c.Redirect("/")
			}
			defer stmt.Close()

			_, err = stmt.Exec(sessionID)
			if err != nil {
				log.Println(err)
			}
		}

		c.ClearCookie("session_id")
		return c.Redirect("/")
	})

	app.Post("/loginadmin", func(c *fiber.Ctx) error {
		password := GetPassword()

		if c.FormValue("password") == password {
			log.Println("Admin login successful")
			sessionID := uuid.New().String()
			expiryTime := time.Now().Add(24 * time.Hour)

			dbMutex.Lock()
			defer dbMutex.Unlock()

			stmt, err := db.Prepare("INSERT INTO sessions (session_id, expiry_time) VALUES (?, ?)")
			if err != nil {
				log.Println(err)
				return c.Redirect("/loginadmin")
			}
			defer stmt.Close()

			_, err = stmt.Exec(sessionID, expiryTime)
			if err != nil {
				log.Println(err)
				return c.Redirect("/loginadmin")
			}

			c.Cookie(&fiber.Cookie{
				Name:     "session_id",
				Value:    sessionID,
				Expires:  expiryTime,
				HTTPOnly: true,
			})

			return c.Redirect("/admin")
		} else {
			log.Println("Admin login failed")
			return c.Redirect("/loginadmin")
		}
	})

	app.Post("/shorten", func(c *fiber.Ctx) error {
		current_user := c.Locals("current_user")

		if current_user == nil {
			log.Println("Admin login required")
			return c.JSON(fiber.Map{"error": "Admin login required"})
		}
		dbMutex.Lock()
		defer dbMutex.Unlock()

		stmt, err := db.Prepare("INSERT INTO links (short_id, long_url) VALUES (?, ?)")
		if err != nil {
			log.Println(err)
			return c.JSON(fiber.Map{"error": "Internal Server Error"})
		}
		defer stmt.Close()

		shortID := GenerateShortID()
		_, err = stmt.Exec(shortID, c.FormValue("long_url"))
		if err != nil {
			log.Println(err)
			return c.JSON(fiber.Map{"error": "Internal Server Error"})
		}

		return c.JSON(fiber.Map{"short_id": shortID})
	})

	app.Get("/delete/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		dbMutex.Lock()
		defer dbMutex.Unlock()

		stmt, err := db.Prepare("DELETE FROM links WHERE id = ?")
		if err != nil {
			log.Println(err)
			return c.Redirect("/admin")
		}
		defer stmt.Close()

		_, err = stmt.Exec(id)
		if err != nil {
			log.Println(err)
			return c.Redirect("/admin")
		}

		return c.Redirect("/admin")
	})

	app.Get("/:short_id", func(c *fiber.Ctx) error {
		shortID := c.Params("short_id")
		var link Link
		err := db.QueryRow("SELECT id, short_id, long_url, created_at FROM links WHERE short_id = ?", shortID).Scan(&link.ID, &link.ShortID, &link.LongURL, &link.CreatedAt)
		if err != nil {
			log.Println(err)
			return c.Render("404", fiber.Map{})
		}
		return c.Redirect(link.LongURL)
	})

	log.Printf("Listening on port %s", PORT)
	log.Fatal(app.Listen(fmt.Sprintf("127.0.0.1:%s", PORT)))
}

// Helper function to get environment variables with a fallback value
func GetEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}
	return value
}

// Generate a random base32 string of length 6 for creating "short links"
func GenerateShortID() string {
	charset := "abcdefghijklmnopqrstuvwxyz0123456789"
	length := 6
	randomString := make([]byte, length)
	for i := range randomString {
		randomString[i] = charset[rand.Intn(len(charset))]
	}
	return string(randomString)
}

func currentUserMiddleware(db *sql.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		sessionID := c.Cookies("session_id")
		if sessionID == "" {
			c.Locals("current_user", nil)
			return c.Next()
		}

		var exists int
		err := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE session_id = ? AND expiry_time > ?", sessionID, time.Now()).Scan(&exists)
		if err != nil || exists == 0 {
			c.Locals("current_user", nil)
			return c.Next()
		}

		c.Locals("current_user", 1)
		return c.Next()
	}
}

func GetPassword() string {
	return GetEnv("ADMIN_PASSWORD", "admin")
}
