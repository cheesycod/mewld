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
	"unicode"

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

func checkAuth(webData WebData, c *gin.Context) *loginDat {
	// Get 'session' cookie
	session, err := c.Cookie("session")

	if err != nil {
		return nil
	}

	// Check session on redis
	redisDat := webData.InstanceList.Redis.Get(webData.InstanceList.Ctx, session).Val()

	if redisDat == "" {
		return nil
	}

	var sess loginDat

	err = json.Unmarshal([]byte(redisDat), &sess)

	if err != nil {
		return nil
	}

	var allowed bool
	for _, id := range webData.InstanceList.Config.AllowedIDS {
		if sess.ID == id {
			allowed = true
			break
		}
	}

	if !allowed {
		log.Error("User not allowed")
		return nil
	}

	return &sess
}

func loginRoute(webData WebData, f func(c *gin.Context, sess *loginDat)) func(c *gin.Context) {
	return func(c *gin.Context) {
		session := checkAuth(webData, c)

		if session != nil {
			f(c, session)
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

type loginDat struct {
	ID          string `json:"id"`
	AccessToken string `json:"access_token"`
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

// pcfgconf is a struct containing the format of a permission config file
type pcfgconf struct {
	CmdName string   `json:"name"`  // The command name
	Roles   []string `json:"roles"` // Variable names of the roles that should be allowed to use this command
}

// pcfg is a struct containing the format of a entire config file
type pcfg struct {
	Config []pcfgconf
	Vars   []string // Variables for use in pcfgconfig
}

func StartWebserver(webData WebData) {
	// Create webserver using gin
	r := gin.New()

	r.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		if param.Path == "/ping" {
			return ""
		}

		// your custom format
		return fmt.Sprintf("%s - \"%s %s %d %s \"%s\" %s\"\n",
			param.ClientIP,
			param.Method,
			param.Path,
			param.StatusCode,
			param.Latency,
			param.Request.UserAgent(),
			param.ErrorMessage,
		)
	}))
	r.Use(gin.Recovery())

	r.GET("/", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			templates.Lookup("index").Execute(c.Writer, webData)
		},
	))

	r.GET("/ping", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			c.String(200, "pong")
		},
	))

	r.GET("/instance-list", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			c.JSON(200, webData.InstanceList)
		},
	))

	r.GET("/action-logs", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			payload := webData.InstanceList.Redis.Get(webData.InstanceList.Ctx, webData.InstanceList.Config.RedisChannel+"_action").Val()

			c.Header("Content-Type", "text/json")

			c.String(200, payload)
		},
	))

	r.POST("/restart", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			webData.InstanceList.SendMessage("0", "", "launcher", "restartproc")
		},
	))

	r.POST("/addtemplate", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			templName := c.Query("name")

			templateFileList = append(templateFileList, templName)
		},
	))

	r.POST("/removetemplate", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
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

	r.GET("/configs", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			// Get every folder in ~/mewld-pconfig
			dirname, err := os.UserHomeDir()

			if err != nil {
				c.JSON(400, gin.H{
					"message": "Could not get user home dir",
				})
				return
			}

			files, err := os.ReadDir(dirname + "/mewld-pconfig")

			if err != nil {
				c.JSON(400, gin.H{
					"message": err.Error(),
				})
				return
			}

			var cfgs = make(map[string]pcfg)

			for _, file := range files {
				log.Info(file.Name())
				cfg := pcfg{}

				// Read vars file
				varsB, err := os.ReadFile(dirname + "/mewld-pconfig/" + file.Name() + "/vars")

				if err != nil {
					c.JSON(400, gin.H{
						"message": err.Error(),
					})
					return
				}

				var vars []string

				// Parse vars
				err = json.Unmarshal(varsB, &vars)

				if err != nil {
					c.JSON(400, gin.H{
						"message": err.Error(),
					})
					return
				}

				cfg.Vars = vars

				// Read perms file
				permsB, err := os.ReadFile(dirname + "/mewld-pconfig/" + file.Name() + "/perms")

				if err != nil {
					c.JSON(400, gin.H{
						"message": err.Error(),
					})
					return
				}

				var config []pcfgconf

				// Parse perms
				err = json.Unmarshal(permsB, &config)

				if err != nil {
					c.JSON(400, gin.H{
						"message": err.Error(),
					})
					return
				}

				cfg.Config = config

				cfgs[file.Name()] = cfg
			}

			c.JSON(200, cfgs)
		},
	))

	r.POST("/configs/vars", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			cfgFile := c.Query("cfg")

			for _, ch := range cfgFile {
				if !unicode.IsLetter(ch) && !unicode.IsNumber(ch) && ch != '_' {
					c.JSON(400, gin.H{
						"message": "name cannot contain non letters/numbers",
					})
					return
				}
			}

			dirname, err := os.UserHomeDir()

			if err != nil {
				c.JSON(400, gin.H{
					"message": "Could not get user home dir",
				})
				return
			}

			// Check if the config exists
			if _, err := os.Stat(dirname + "/mewld-pconfig/" + cfgFile); err != nil {
				c.JSON(400, gin.H{
					"message": "Could not get config: " + err.Error(),
				})
				return
			}

			// Read vars file
			varsB, err := os.ReadFile(dirname + "/mewld-pconfig/" + cfgFile + "/vars")

			if err != nil {
				c.JSON(400, gin.H{
					"message": err.Error(),
				})
				return
			}

			var vars []string

			// Parse vars
			err = json.Unmarshal(varsB, &vars)

			if err != nil {
				c.JSON(400, gin.H{
					"message": err.Error(),
				})
				return
			}

			name := c.Query("name")

			if name == "" {
				c.JSON(400, gin.H{
					"message": "name cannot be empty",
				})
				return
			}

			newVars := []string{}

			if strings.HasPrefix(name, "-") {
				// Remove var
				nameClean := strings.TrimPrefix(name, "-")
				for _, v := range vars {
					if v != nameClean {
						newVars = append(newVars, v)
						break
					}
				}

			} else {
				// Add var
				newVars = append(vars, name)
			}

			bytes, err := json.Marshal(newVars)

			if err != nil {
				c.JSON(400, gin.H{
					"message": err.Error(),
				})
				return
			}

			// Write vars file
			os.WriteFile(dirname+"/mewld-pconfig/"+cfgFile+"/vars", bytes, 0644)
		},
	))

	r.POST("/configs", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			cfgFile := c.Query("name")

			for _, ch := range cfgFile {
				if !unicode.IsLetter(ch) && !unicode.IsNumber(ch) && ch != '_' {
					c.JSON(400, gin.H{
						"message": "name cannot contain non letters/numbers",
					})
					return
				}
			}

			dirname, err := os.UserHomeDir()

			if err != nil {
				c.JSON(400, gin.H{
					"message": "Could not get user home dir",
				})
				return
			}

			// Check if cfgFile exists and that it is a directory
			if fi, err := os.Stat(dirname + "/mewld-pconfig"); err != nil {
				// Create the config folder
				err = os.Mkdir(dirname+"/mewld-pconfig", 0755)

				if err != nil {
					c.JSON(400, gin.H{
						"message": err.Error(),
					})
					return
				}
			} else {
				if !fi.IsDir() {
					// Remove the file
					err = os.Remove(dirname + "/mewld-pconfig")

					if err != nil {
						c.JSON(400, gin.H{
							"message": err.Error(),
						})
						return
					}

					// Create the config folder
					err = os.Mkdir(dirname+"/mewld-pconfig", 0755)

					if err != nil {
						c.JSON(400, gin.H{
							"message": err.Error(),
						})
						return
					}
				}
			}

			// Check if the config exists
			if fi, err := os.Stat(dirname + "/mewld-pconfig/" + cfgFile); err == nil {
				if !fi.IsDir() {
					// Remove the file
					err = os.Remove(dirname + "/mewld-pconfig/" + cfgFile)

					if err != nil {
						c.JSON(400, gin.H{
							"message": err.Error(),
						})
						return
					}
				} else {
					c.JSON(400, gin.H{
						"message": "Config already exists",
					})
					return
				}
			}

			os.Mkdir(dirname+"/mewld-pconfig/"+cfgFile, 0755)

			os.WriteFile(dirname+"/mewld-pconfig/"+cfgFile+"/vars", []byte("[]"), 0644)
			os.WriteFile(dirname+"/mewld-pconfig/"+cfgFile+"/perms", []byte("[]"), 0644)

			c.String(200, "OK")
		},
	))

	r.GET("/cperms", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			// /applications/{application.id}/guilds/{guild.id}/commands/{command.id}/permissions

			guildId := c.Query("guildId")

			if guildId == "" {
				c.JSON(400, gin.H{
					"message": "guildId is required",
				})
				return
			}

			commandId := c.Query("commandId")

			if commandId == "" {
				c.JSON(400, gin.H{
					"message": "commandId is required",
				})
				return
			}

			res, err := http.NewRequest(
				"GET",
				"https://discord.com/api/v10/applications/"+webData.InstanceList.Config.Oauth.ClientID+"/guilds/"+guildId+"/commands/"+commandId+"/permissions",
				nil,
			)

			if err != nil {
				log.Error(err)
				c.String(500, err.Error())
				return
			}

			res.Header.Set("Authorization", "Bot "+os.Getenv("MTOKEN"))
			res.Header.Set("User-Agent", "DiscordBot (WebUI/1.0)")

			client := &http.Client{Timeout: time.Second * 10}

			resp, err := client.Do(res)

			if err != nil {
				log.Error(err)
				c.String(500, err.Error())
				return
			}

			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)

			if err != nil {
				log.Error(err)
				c.String(500, err.Error())
				return
			}

			c.Header("Content-Type", "application/json")
			c.String(200, string(body))
		},
	))

	r.GET("/commands", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			//"/applications/{application.id}/guilds/{guild.id}/commands"

			guildId := c.Query("guildId")

			if guildId == "" {
				c.JSON(400, gin.H{
					"message": "guildId is required",
				})
				return
			}

			res, err := http.NewRequest(
				"GET",
				"https://discord.com/api/v10/applications/"+webData.InstanceList.Config.Oauth.ClientID+"/guilds/"+guildId+"/commands",
				nil,
			)

			if err != nil {
				log.Error(err)
				c.String(500, err.Error())
				return
			}

			res.Header.Set("Authorization", "Bot "+os.Getenv("MTOKEN"))
			res.Header.Set("User-Agent", "DiscordBot (WebUI/1.0)")

			client := &http.Client{Timeout: time.Second * 10}

			resp, err := client.Do(res)

			if err != nil {
				log.Error(err)
				c.String(500, err.Error())
				return
			}

			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)

			if err != nil {
				log.Error(err)
				c.String(500, err.Error())
				return
			}

			c.Header("Content-Type", "application/json")
			c.String(200, string(body))
		},
	))

	r.GET("/guilds", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			// Check for guilds on redis
			redisGuilds := webData.InstanceList.Redis.Get(webData.InstanceList.Ctx, sess.ID+"_guilds").Val()

			if redisGuilds != "" {
				c.Header("Content-Type", "application/json")
				c.Header("X-Cached", "true")
				c.String(200, redisGuilds)
				return
			}

			res, err := http.NewRequest("GET", "https://discord.com/api/v10/users/@me/guilds", nil)

			if err != nil {
				log.Error(err)
				c.String(500, err.Error())
				return
			}

			res.Header.Set("Authorization", "Bearer "+sess.AccessToken)

			client := &http.Client{Timeout: time.Second * 10}

			resp, err := client.Do(res)

			if err != nil {
				log.Error(err)
				c.String(500, err.Error())
				return
			}

			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)

			if err != nil {
				log.Error(err)
				c.String(500, err.Error())
				return
			}

			webData.InstanceList.Redis.Set(webData.InstanceList.Ctx, sess.ID+"_guilds", string(body), 5*time.Minute).Val()

			c.Header("Content-Type", "application/json")
			c.String(200, string(body))
		},
	))

	r.GET("/reload", loginRoute(
		webData,
		func(c *gin.Context, sess *loginDat) {
			// Reload all templates from their files
			dirname, err := os.UserHomeDir()

			if err != nil {
				log.Error(err)
				c.String(http.StatusInternalServerError, "message")
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
		c.Redirect(302, "https://discord.com/api/oauth2/authorize?client_id="+webData.InstanceList.Config.Oauth.ClientID+"&redirect_uri="+webData.InstanceList.Config.Oauth.RedirectURL+"/confirm&response_type=code&scope=identify%20guilds%20applications.commands.permissions.update&state="+c.Query("redirect"))
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

		jsonStruct := loginDat{
			ID:          discordUser.ID,
			AccessToken: discordToken.AccessToken,
		}

		jsonBytes, err := json.Marshal(jsonStruct)

		if err != nil {
			log.Error(err)
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		webData.InstanceList.Redis.Set(webData.InstanceList.Ctx, sessionTok, string(jsonBytes), time.Minute*30)

		// Set cookie
		c.SetCookie("session", sessionTok, int(time.Hour.Seconds()), "/", "", false, true)

		// Redirect to dashboard
		c.Redirect(302, "/")
	})

	r.Run("0.0.0.0:1293") // listen and serve
}
