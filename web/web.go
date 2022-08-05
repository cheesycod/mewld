package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mewld/proc"
	"mewld/redis"
	"net/http"
	"os"
	"time"

	_ "embed"

	"github.com/gin-gonic/gin"
)

// Template files
var (
	//go:embed webui/index.html
	indexTemplate string
)

var templates *template.Template

func init() {
	// Load template from string literal
	templates = template.Must(
		template.New("index").Parse(indexTemplate))
}

type SessionStartLimit struct {
	Total          uint64 `json:"total"`
	Remaining      uint64 `json:"remaining"`
	ResetAfter     uint64 `json:"reset_after"`
	MaxConcurrency uint64 `json:"max_concurrency"`
}

type ShardCount struct {
	Shards            uint64            `json:"shards"`
	SessionStartLimit SessionStartLimit `json:"session_start_limit"`
}

func GetShardCount() ShardCount {
	url := "https://discord.com/api/gateway/bot"

	req, err := http.NewRequest("GET", url, nil)

	req.Header.Add("Authorization", "Bot "+os.Getenv("MTOKEN"))
	req.Header.Add("User-Agent", "MewBot/1.0")
	req.Header.Add("Content-Type", "application/json")

	if err != nil {
		log.Fatal(err)
	}

	client := http.Client{Timeout: 10 * time.Second}

	res, err := client.Do(req)

	if err != nil {
		log.Fatal(err)
	}

	defer res.Body.Close()

	log.Println("Shard count status:", res.Status)

	if res.StatusCode != 200 {
		log.Fatal("Shard count status code not 200. Invalid token?")
	}

	var shardCount ShardCount

	bodyBytes, err := io.ReadAll(res.Body)

	if err != nil {
		log.Fatal(err)
	}

	err = json.Unmarshal(bodyBytes, &shardCount)

	if err != nil {
		log.Fatal(err)
	}

	if shardCount.SessionStartLimit.Remaining < 10 {
		log.Fatal("Shard count remaining is less than safe value of 10")
	}

	return shardCount
}

type WebData struct {
	RedisHandler *redis.RedisHandler
	InstanceList *proc.InstanceList
}

func checkAuth(c *gin.Context) string {
	// Get 'session' cookie
	session, err := c.Cookie("session")

	if err != nil {
		return ""
	}

	return session
}

func loginRoute(f func(c *gin.Context)) func(c *gin.Context) {
	return func(c *gin.Context) {
		session := checkAuth(c)

		if session != "" {
			f(c)
			return
		}

		c.Redirect(302, "/login?redirect="+c.Request.URL.Path)
	}
}

func StartWebserver(webData WebData) {
	// Create webserver using gin
	r := gin.New()

	r.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {

		// your custom format
		return fmt.Sprintf("%s - [%s] \"%s %s %s %d %s \"%s\" %s\"\n",
			param.ClientIP,
			param.TimeStamp.Format(time.RFC1123),
			param.Method,
			param.Path,
			param.Request.Proto,
			param.StatusCode,
			param.Latency,
			param.Request.UserAgent(),
			param.ErrorMessage,
		)
	}))
	r.Use(gin.Recovery())

	r.GET("/", loginRoute(
		func(c *gin.Context) {
			templates.Lookup("index").Execute(c.Writer, webData)
		},
	))

	r.GET("/login", func(c *gin.Context) {
		// Redirect via discord oauth2
	})

	r.Run("0.0.0.0:1293") // listen and serve
}
