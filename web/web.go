package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"mewld/coreutils"
	"mewld/proc"
	"mewld/redis"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/gin-gonic/gin"
)

// Template files
var (
	//go:embed webui/*.html
	templateFiles embed.FS
	templates     *template.Template

	templateFileList = []string{
		"head",
	}
)

func init() {
	// Load template from string literal

	// Index template
	fileContent, err := templateFiles.ReadFile("webui/index.html")

	if err != nil {
		panic(err)
	}

	templates = template.Must(template.New("index").Parse(string(fileContent)))

	for _, t := range templateFileList {
		fileContent, err := templateFiles.ReadFile("webui/" + t + ".html")

		if err != nil {
			panic(err)
		}

		template.Must(templates.New(t).Parse(string(fileContent)))
	}
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

	// Check session on redis

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

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

type user struct {
	ID string `json:"id"`
}

func templParse(filename string) string {
	// Open file
	file, err := os.Open(filename)

	if err != nil {
		log.Fatal(err)
	}

	// Read file
	fileBytes, err := io.ReadAll(file)

	if err != nil {
		log.Error(err)
	}

	return string(fileBytes)
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

	r.GET("/ping", func(c *gin.Context) {
		c.String(200, "pong")
	})

	r.GET("/instance-list", loginRoute(
		func(c *gin.Context) {
			c.JSON(200, webData.InstanceList)
		},
	))

	r.POST("/restart", loginRoute(
		func(c *gin.Context) {
			webData.InstanceList.SendMessage("0", "", "launcher", "restartproc")
		},
	))

	r.POST("/addtemplate", loginRoute(
		func(c *gin.Context) {
			templName := c.Query("name")

			templateFileList = append(templateFileList, templName)
		},
	))

	r.POST("/removetemplate", loginRoute(
		func(c *gin.Context) {
			templName := c.Query("name")

			templateFileList = []string{}

			for _, t := range templateFileList {
				if t != templName {
					templateFileList = append(templateFileList, t)
					break
				}
			}
		},
	))

	r.GET("/reload", loginRoute(
		func(c *gin.Context) {
			// Reload all templates from their files
			dirname, err := os.UserHomeDir()

			if err != nil {
				log.Error(err)
				c.String(http.StatusInternalServerError, "Error")
			}

			var newTemplate *template.Template

			templates = newTemplate // Reset template

			templates = template.Must(template.New("index").Parse(templParse(dirname + "/mewld/web/webui/index.html")))

			for _, t := range templateFileList {
				templates = template.Must(templates.New(t).Parse(templParse(dirname + "/mewld/web/webui/" + t + ".html")))
			}
		},
	))

	r.GET("/login", func(c *gin.Context) {
		// Redirect via discord oauth2
		c.Redirect(302, "https://discord.com/api/oauth2/authorize?client_id="+webData.InstanceList.Config.Oauth.ClientID+"&redirect_uri="+webData.InstanceList.Config.Oauth.RedirectURL+"/confirm&response_type=code&scope=identify&state="+c.Query("redirect"))
	})

	r.GET("/confirm", func(c *gin.Context) {
		// Handle confirmation from discord oauth2
		code := c.Query("code")

		// Add form data
		form := url.Values{}
		form["client_id"] = []string{webData.InstanceList.Config.Oauth.ClientID}
		form["client_secret"] = []string{webData.InstanceList.Config.Oauth.ClientSecret}
		form["grant_type"] = []string{"authorization_code"}
		form["code"] = []string{code}
		form["redirect_uri"] = []string{webData.InstanceList.Config.Oauth.RedirectURL + "/confirm"}

		req, err := http.NewRequest("POST", "https://discord.com/api/oauth2/token", strings.NewReader(form.Encode()))

		if err != nil {
			log.Error(err)
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		// Set headers
		req.Header.Add("User-Agent", "Mewld-webui/1.0")
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

		// Create client
		client := http.Client{Timeout: 10 * time.Second}

		// Do request
		res, err := client.Do(req)

		if err != nil {
			log.Error(err)
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		// Read response
		bodyBytes, err := io.ReadAll(res.Body)

		log.Info(string(bodyBytes))

		if err != nil {
			log.Error(err)
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		// Parse response
		var discordToken tokenResponse

		err = json.Unmarshal(bodyBytes, &discordToken)

		if err != nil {
			log.Error(err)
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		// Close body
		res.Body.Close()

		log.Info("Access Token: ", discordToken.AccessToken)

		// Get user info and create session cookie
		req, err = http.NewRequest("GET", "https://discord.com/api/users/@me", nil)

		if err != nil {
			log.Error(err)
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		// Set headers
		req.Header.Add("User-Agent", "Mewld-webui/1.0")
		req.Header.Add("Authorization", "Bearer "+discordToken.AccessToken)

		// Do request
		res, err = client.Do(req)

		if err != nil {
			log.Error(err)
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		// Read response
		bodyBytes, err = io.ReadAll(res.Body)

		if err != nil {
			log.Error(err)
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		// Parse response
		var discordUser user

		err = json.Unmarshal(bodyBytes, &discordUser)

		if err != nil {
			log.Error(err)
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		log.Info("User Data: ", discordUser)

		var allowed bool
		for _, id := range webData.InstanceList.Config.AllowedIDS {
			if discordUser.ID == id {
				allowed = true
				break
			}
		}

		if !allowed {
			log.Error("User not allowed")
			c.String(http.StatusInternalServerError, "User not allowed")
			return
		}

		sessionTok := coreutils.RandomString(64)

		webData.InstanceList.Redis.Set(webData.InstanceList.Ctx, sessionTok, discordUser.ID, time.Minute*15)

		// Set cookie
		c.SetCookie("session", sessionTok, int(time.Hour.Seconds()), "/", "", false, true)

		// Redirect to dashboard
		c.Redirect(302, "/")
	})

	r.Run("0.0.0.0:1293") // listen and serve
}
