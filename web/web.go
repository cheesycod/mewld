package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cheesycod/mewld/proc"
	"github.com/cheesycod/mewld/redis"
	"github.com/cheesycod/mewld/utils"

	log "github.com/sirupsen/logrus"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type WebData struct {
	RedisHandler *redis.RedisHandler
	InstanceList *proc.InstanceList
}

func checkAuth(webData WebData, r *http.Request) *loginDat {
	// Get 'session' cookie
	sessionData := r.Header.Get("X-Session")

	if sessionData == "" {
		sessionCookie, err := r.Cookie("session")

		if err != nil {
			return nil
		}

		sessionData = sessionCookie.Value
	}

	// Check session on redis
	redisDat := webData.InstanceList.Redis.Get(webData.InstanceList.Ctx, sessionData).Val()

	if redisDat == "" {
		return nil
	}

	var sess loginDat

	err := json.Unmarshal([]byte(redisDat), &sess)

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

func loginRoute(webData WebData, f func(w http.ResponseWriter, r *http.Request, sess *loginDat)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		session := checkAuth(webData, r)

		if session != nil {
			f(w, r, session)
			return
		}

		http.Redirect(w, r, "/login?redirect="+r.URL.Path, http.StatusFound)
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

func StartWebserver(webData WebData) http.Server {
	// Create webserver using gin
	r := chi.NewMux()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Mewld instance up, use mewld-ui to access it using your browser"))
	})

	r.Get("/ping", loginRoute(
		webData,
		func(w http.ResponseWriter, r *http.Request, sessData *loginDat) {
			w.Write([]byte("pong"))
		},
	))

	r.Get("/instance-list", loginRoute(
		webData,
		func(w http.ResponseWriter, r *http.Request, sessData *loginDat) {
			err := json.NewEncoder(w).Encode(webData.InstanceList)

			if err != nil {
				w.Write([]byte(err.Error()))
				w.WriteHeader(http.StatusInternalServerError)
			}
		},
	))

	r.Get("/action-logs", loginRoute(
		webData,
		func(w http.ResponseWriter, r *http.Request, sessData *loginDat) {
			payload := webData.InstanceList.Redis.LRange(webData.InstanceList.Ctx, webData.InstanceList.Config.RedisChannel+"/actlogs", 0, -1).Val()

			var payloadFinal []map[string]any

			for i, p := range payload {
				var pm map[string]any

				err := json.Unmarshal([]byte(p), &pm)

				if err != nil {
					w.Write([]byte(fmt.Sprintf("Could not unmarshal payload %d: %s", i, err.Error())))
					return
				}

				payloadFinal = append(payloadFinal, pm)
			}

			err := json.NewEncoder(w).Encode(payloadFinal)

			if err != nil {
				w.Write([]byte(err.Error()))
				w.WriteHeader(http.StatusInternalServerError)
			}
		},
	))

	r.Post("/redis/pub", loginRoute(
		webData,
		func(w http.ResponseWriter, r *http.Request, sessData *loginDat) {
			payload, err := io.ReadAll(r.Body)

			if err != nil {
				log.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Error reading body:" + err.Error()))
				return
			}

			webData.InstanceList.Redis.Publish(webData.InstanceList.Ctx, webData.InstanceList.Config.RedisChannel, string(payload))
		},
	))

	r.Get("/cluster-health", loginRoute(
		webData,
		func(w http.ResponseWriter, r *http.Request, sess *loginDat) {
			var cid = r.URL.Query().Get("cid")

			if cid == "" {
				cid = "0"
			}

			cInt, err := strconv.Atoi(cid)

			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("{\"error\": \"Invalid cid, could not parse as int\"}"))
				return
			}

			instance := webData.InstanceList.InstanceByID(cInt)

			if instance == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("{\"error\": \"Invalid cid, no such instance\"}"))
				return
			}

			if instance.ClusterHealth == nil {
				if !instance.LaunchedFully {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte("{\"error\": \"Instance not fully launched\"}"))
					return
				}
				ch, err := webData.InstanceList.ScanShards(instance)

				if err != nil {
					var m = map[string]string{
						"error": "Error scanning shards: " + err.Error(),
					}

					bytes, err := json.Marshal(m)

					if err != nil {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusInternalServerError)
						w.Write([]byte("{\"error\": \"Error scanning shards: unable to unmarshal payload\"}"))
						return
					}

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					w.Write(bytes)
					return
				}

				instance.ClusterHealth = ch
			}

			data := map[string]any{
				"locked": instance.Locked(),
				"health": instance.ClusterHealth,
			}

			bytes, err := json.Marshal(data)

			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("{\"error\": \"Error marshalling data: unable to unmarshal payload\"}"))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write(bytes)
		},
	))

	r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
		// Redirect via discord oauth2
		url := "https://discord.com/api/oauth2/authorize?client_id=" + webData.InstanceList.Config.Oauth.ClientID + "&redirect_uri=" + webData.InstanceList.Config.Oauth.RedirectURL + "/confirm&response_type=code&scope=identify%20guilds%20applications.commands.permissions.update&state=" + r.URL.Query().Get("api")

		// For upcoming sveltekit webui rewrite
		if r.URL.Query().Get("api") == "" {
			http.Redirect(w, r, url, http.StatusFound)
		} else {
			w.Write([]byte(url))
		}
	})

	r.Get("/confirm", func(w http.ResponseWriter, r *http.Request) {
		// Handle confirmation from discord oauth2
		code := r.URL.Query().Get("code")

		state := r.URL.Query().Get("state")

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
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
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
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		// Read response
		bodyBytes, err := io.ReadAll(res.Body)

		log.Info(string(bodyBytes))

		if err != nil {
			log.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		// Parse response
		var discordToken tokenResponse

		err = json.Unmarshal(bodyBytes, &discordToken)

		if err != nil {
			log.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		// Close body
		res.Body.Close()

		// Get user info and create session cookie
		req, err = http.NewRequest("GET", "https://discord.com/api/users/@me", nil)

		if err != nil {
			log.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		// Set headers
		req.Header.Add("User-Agent", "Mewld-webui/1.0")
		req.Header.Add("Authorization", "Bearer "+discordToken.AccessToken)

		// Do request
		res, err = client.Do(req)

		if err != nil {
			log.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		// Read response
		bodyBytes, err = io.ReadAll(res.Body)

		if err != nil {
			log.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		// Parse response
		var discordUser user

		err = json.Unmarshal(bodyBytes, &discordUser)

		if err != nil {
			log.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
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
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("User not allowed"))
			return
		}

		sessionTok := utils.RandomString(64)

		jsonStruct := loginDat{
			ID:          discordUser.ID,
			AccessToken: discordToken.AccessToken,
		}

		jsonBytes, err := json.Marshal(jsonStruct)

		if err != nil {
			log.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		webData.InstanceList.Redis.Set(webData.InstanceList.Ctx, sessionTok, string(jsonBytes), time.Minute*30)

		// Set cookie "session"
		c := http.Cookie{
			Name:    "session",
			Value:   sessionTok,
			Expires: time.Now().Add(time.Minute * 30),
			Path:    "/",
		}

		http.SetCookie(w, &c)

		if strings.HasPrefix(state, "api") {
			split := strings.Split(state, "@")

			if len(split) != 3 {
				log.Error("Invalid state")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Invalid state"))
				return
			}

			url := split[1]
			iUrl := split[2]

			http.Redirect(w, r, url+"/ss?session="+sessionTok+"&instanceUrl="+iUrl, http.StatusFound)
			return
		}

		// Redirect to dashboard
		http.Redirect(w, r, "/", http.StatusFound)
	})

	return http.Server{
		Addr:    ":1293",
		Handler: r,
	}
}
