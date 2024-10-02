package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

type UserCredential struct {
	Email    string
	APIToken string
}

func main() {

	// Start the Hello World API
	http.HandleFunc("/", helloHandler)
	log.Println("\033[1;34mStarting Hello World API on :8009\033[0m")
	go func() {
		if err := http.ListenAndServe(":8009", nil); err != nil {
			log.Fatalf("\033[1;31mError starting Hello World API: %v\033[0m", err)
		}
	}()

	log.Println("\033[1;34mStarting Slack sign-in process\033[0m")

	// Open SQLite database
	db, err := sql.Open("sqlite3", "./slack_users.db")
	if err != nil {
		log.Fatalf("\033[1;31mError opening database: %v\033[0m", err)
	}
	defer db.Close()

	// Create table if not exists
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		email TEXT PRIMARY KEY,
		api_token TEXT
	)`)
	if err != nil {
		log.Fatalf("\033[1;31mError creating table: %v\033[0m", err)
	}

	// List all created users
	rows, err := db.Query("SELECT email, api_token FROM users")
	if err != nil {
		log.Fatalf("\033[1;31mError querying users: %v\033[0m", err)
	}
	defer rows.Close()

	log.Println("\033[1;32mExisting users:\033[0m")
	for rows.Next() {
		var user UserCredential
		err := rows.Scan(&user.Email, &user.APIToken)
		if err != nil {
			log.Printf("\033[1;31mError scanning row: %v\033[0m", err)
			continue
		}
		log.Printf("\033[1;36mEmail: %s, API Token: %s\033[0m", user.Email, user.APIToken)
	}

	// Ask user if they want to create a new user
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\033[1;32mDo you want to create a new user? (y/n): \033[0m")
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer == "y" || answer == "yes" {
		// Set up Slack sign-in
		source := rand.NewSource(time.Now().UnixNano())
		r := rand.New(source)
		randomNum := r.Intn(100000000)
		email := fmt.Sprintf("users+%08d@slack.adi.fr.eu.org ", randomNum)
		workspace := "dogcattalk" // Replace with actual workspace name
		log.Printf("\033[1;36mUsing email: %s for workspace: %s\033[0m", email, workspace)

		// Base URL for Slack API
		baseURL := fmt.Sprintf("https://%s.slack.com/api", workspace)

		// Initialize cookies
		cookies := make([]*http.Cookie, 0)

		// Check if email is available for signup
		checkEmailURL := fmt.Sprintf("%s/signup.checkEmail", baseURL)
		checkEmailData := url.Values{}
		checkEmailData.Set("email", email)
		log.Printf("\033[1;33mChecking email availability at: %s\033[0m", checkEmailURL)
		resp, err := sendRequest(http.MethodPost, checkEmailURL, checkEmailData, cookies)
		if err != nil {
			log.Fatalf("\033[1;31mError checking email: %v\033[0m", err)
		}
		defer resp.Body.Close()
		cookies = updateCookies(cookies, resp.Cookies())
		logResponse("Email check", resp)

		// Confirm email address for signup
		confirmEmailURL := fmt.Sprintf("%s/signup.confirmEmail", baseURL)
		confirmEmailData := url.Values{}
		confirmEmailData.Set("email", email)
		confirmEmailData.Set("locale", "en-US")
		log.Printf("\033[1;33mConfirming email at: %s\033[0m", confirmEmailURL)
		resp, err = sendRequest(http.MethodPost, confirmEmailURL, confirmEmailData, cookies)
		if err != nil {
			log.Fatalf("\033[1;31mError confirming email: %v\033[0m", err)
		}
		defer resp.Body.Close()
		cookies = updateCookies(cookies, resp.Cookies())
		logResponse("Email confirmation", resp)

		// Prompt user for verification code
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("\033[1;32mEnter the verification code sent to your email: \033[0m")
		confirmationCode, _ := reader.ReadString('\n')
		confirmationCode = confirmationCode[:len(confirmationCode)-1] // Remove newline character
		log.Printf("\033[1;36mUsing confirmation code: %s\033[0m", confirmationCode)
		// Confirm verification code
		signInURL := fmt.Sprintf("%s/signin.confirmCode", baseURL)
		signInData := url.Values{}
		signInData.Set("email", email)
		signInData.Set("code", strings.ReplaceAll(confirmationCode, "-", ""))
		log.Printf("\033[1;33mConfirming code at: %s\033[0m", signInURL)
		resp, err = sendRequest(http.MethodPost, signInURL, signInData, cookies)
		if err != nil {
			log.Fatalf("\033[1;31mError confirming code: %v\033[0m", err)
		}
		defer resp.Body.Close()
		cookies = updateCookies(cookies, resp.Cookies())
		logResponse("Confirm code", resp)

		// Check if code confirmation was successful
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatalf("\033[1;31mError reading response body after confirming code: %v\033[0m", err)
		}

		log.Printf("\033[1;35mResponse body after confirming code: %s\033[0m", string(body))

		// Generate full name using randommer.io
		fullName, err := getRandomFullName()
		if err != nil {
			log.Printf("\033[1;31mError generating full name: %v\033[0m", err)
			fullName = fmt.Sprintf("User%08d", randomNum)
		}

		// Create user using signup.createUser API
		createUserURL := "https://dogcattalk.slack.com/api/signup.createUser"
		createUserData := url.Values{}
		createUserData.Set("code", "")
		createUserData.Set("display_name", fullName)
		createUserData.Set("emailok", "true")
		createUserData.Set("join_type", "shared_invite_confirmed")
		createUserData.Set("last_tos_acknowledged", "tos_mar2018")
		createUserData.Set("locale", "en-GB")
		createUserData.Set("real_name", fullName)
		createUserData.Set("shared_invite_code", "zt-2rggdrx7r-gPbD08EqfwfhjluP0B4jNQ")
		createUserData.Set("team", "T07Q4VBFFHP")
		createUserData.Set("tz", "America/Los_Angeles")

		log.Printf("\033[1;33mCreating user at: %s\033[0m", createUserURL)
		resp, err = sendRequest(http.MethodPost, createUserURL, createUserData, cookies)
		if err != nil {
			log.Fatalf("\033[1;31mError creating user: %v\033[0m", err)
		}
		defer resp.Body.Close()
		cookies = updateCookies(cookies, resp.Cookies())
		logResponse("Create user", resp)

		body, err = io.ReadAll(resp.Body)
		if err != nil {
			log.Fatalf("\033[1;31mError reading response body: %v\033[0m", err)
		}

		log.Printf("\033[1;35mCreate user Response Body: %s\033[0m", string(body))

		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			log.Fatalf("\033[1;31mError parsing JSON response: %v\033[0m", err)
		}

		if result["ok"] == true {
			log.Println("\033[1;32mUser creation successful!\033[0m")

			// Extract the API token from the response
			apiToken, ok := result["api_token"].(string)
			if !ok {
				log.Fatalf("\033[1;31mFailed to get API token from create user response\033[0m")
			}

			// Store user credentials in SQLite
			_, err = db.Exec("INSERT INTO users (email, api_token) VALUES (?, ?)", email, apiToken)
			if err != nil {
				log.Printf("\033[1;31mError storing user credentials: %v\033[0m", err)
			} else {
				log.Println("\033[1;32mUser credentials stored successfully\033[0m")
			}

			// Get profile picture from thispersondoesnotexist.com
			log.Printf("\033[1;33mGetting profile picture from thispersondoesnotexist.com\033[0m")
			profilePicResp, err := http.Get("https://thispersondoesnotexist.com")
			if err != nil {
				log.Printf("\033[1;31mError getting profile picture: %v\033[0m", err)
			} else {
				defer profilePicResp.Body.Close()
				profilePicData, err := io.ReadAll(profilePicResp.Body)
				if err != nil {
					log.Printf("\033[1;31mError reading profile picture data: %v\033[0m", err)
				} else {
					// Update profile picture
					updateProfilePicURL := fmt.Sprintf("%s/users.setPhoto", baseURL)
					updateProfilePicData := &bytes.Buffer{}
					writer := multipart.NewWriter(updateProfilePicData)
					part, err := writer.CreateFormFile("image", "profile.jpg")
					if err != nil {
						log.Printf("\033[1;31mError creating form file: %v\033[0m", err)
					} else {
						part.Write(profilePicData)
						writer.WriteField("token", apiToken)
						writer.Close()

						log.Printf("\033[1;33mUpdating profile picture\033[0m")
						updateProfilePicResp, err := http.Post(updateProfilePicURL, writer.FormDataContentType(), updateProfilePicData)
						if err != nil {
							log.Printf("\033[1;31mError updating profile picture: %v\033[0m", err)
						} else {
							defer updateProfilePicResp.Body.Close()
							logResponse("Update profile picture", updateProfilePicResp)
						}
					}
				}
			}

			// Send a message to #cats channel
			sendMessageURL := fmt.Sprintf("%s/chat.postMessage", baseURL)
			message := "Hello, I'm a new user!"
			sendMessageData := url.Values{}
			sendMessageData.Set("token", apiToken)
			sendMessageData.Set("channel", "cats")
			sendMessageData.Set("text", message)

			log.Printf("\033[1;33mSending message to #cats channel\033[0m")
			resp, err = sendRequest(http.MethodPost, sendMessageURL, sendMessageData, cookies)
			if err != nil {
				log.Fatalf("\033[1;31mError sending message: %v\033[0m", err)
			}
			defer resp.Body.Close()
			cookies = updateCookies(cookies, resp.Cookies())
			logResponse("Send message", resp)

		} else {
			log.Println("\033[1;31mUser creation failed.\033[0m")
			log.Printf("\033[1;31mError details: %+v\033[0m", result)
		}
	}

	// Retrieve all saved users
	rows, err = db.Query("SELECT email, api_token FROM users")
	if err != nil {
		log.Fatalf("\033[1;31mError querying users: %v\033[0m", err)
	}
	defer rows.Close()

	var wg sync.WaitGroup

	for rows.Next() {
		var user UserCredential
		err := rows.Scan(&user.Email, &user.APIToken)
		if err != nil {
			log.Printf("\033[1;31mError scanning row: %v\033[0m", err)
			continue
		}

		wg.Add(1)
		go func(email, apiToken string) {
			defer wg.Done()
			handleUserMessages(email, apiToken)
		}(user.Email, user.APIToken)
	}

	wg.Wait()
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, World!")
}

func sendRequest(method, url string, data url.Values, cookies []*http.Cookie) (*http.Response, error) {
	client := &http.Client{}
	req, err := http.NewRequest(method, url, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	return client.Do(req)
}

func updateCookies(existingCookies, newCookies []*http.Cookie) []*http.Cookie {
	cookieMap := make(map[string]*http.Cookie)

	// Add existing cookies to the map
	for _, cookie := range existingCookies {
		cookieMap[cookie.Name] = cookie
	}

	// Update or add new cookies
	for _, cookie := range newCookies {
		if cookie.Name == "b" || cookie.Name == "x" || cookie.Name == "ec" {
			cookieMap[cookie.Name] = cookie
		}
	}

	// Convert map back to slice
	updatedCookies := make([]*http.Cookie, 0, len(cookieMap))
	for _, cookie := range cookieMap {
		updatedCookies = append(updatedCookies, cookie)
	}

	return updatedCookies
}

func logResponse(step string, resp *http.Response) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("\033[1;31mError reading %s response body: %v\033[0m", step, err)
		return
	}
	resp.Body = io.NopCloser(bytes.NewReader(body)) // Reset the body for further use

	log.Printf("\033[1;34m%s Response Status: %s\033[0m", step, resp.Status)
	log.Printf("\033[1;36m%s Response Headers:\033[0m", step)
	for key, values := range resp.Header {
		for _, value := range values {
			log.Printf("  \033[1;36m%s: %s\033[0m", key, value)
		}
	}
	log.Printf("\033[1;35m%s Response Body: %s\033[0m", step, string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("\033[1;31mError parsing %s JSON response: %v\033[0m", step, err)
	} else {
		if result["ok"] != true {
			log.Printf("\033[1;31m%s failed. Error details: %+v\033[0m", step, result)
		} else {
			log.Printf("\033[1;32m%s successful\033[0m", step)
		}
	}
}

func handleUserMessages(email, apiToken string) {
	log.Printf("\033[1;34mStarting message handling for user: %s\033[0m", email)

	// Get WebSocket URL from Slack API
	url := fmt.Sprintf("https://slack.com/api/rtm.connect?token=%s&pretty=1", apiToken)
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("\033[1;31mError fetching WebSocket URL for user %s: %v\033[0m", email, err)
		return
	}
	defer resp.Body.Close()

	var result struct {
		OK   bool   `json:"ok"`
		URL  string `json:"url"`
		Self struct {
			ID string `json:"id"`
		} `json:"self"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("\033[1;31mError decoding response for user %s: %v\033[0m", email, err)
		return
	}

	if !result.OK {
		log.Printf("\033[1;31mSlack API returned non-OK response for user %s\033[0m", email)
		return
	}

	// Connect to WebSocket
	c, _, err := websocket.DefaultDialer.Dial(result.URL, nil)
	if err != nil {
		log.Printf("\033[1;31mError connecting to WebSocket for user %s: %v\033[0m", email, err)
		return
	}
	defer c.Close()

	log.Printf("\033[1;32mWebSocket connection established for user: %s\033[0m", email)

	// Handle incoming messages
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Printf("\033[1;31mError reading message for user %s: %v\033[0m", email, err)
			return
		}

		log.Printf("\033[1;36mReceived event for user %s: %s\033[0m", email, string(message))

		var event map[string]interface{}
		if err := json.Unmarshal(message, &event); err != nil {
			log.Printf("\033[1;31mError parsing message for user %s: %v\033[0m", email, err)
			continue
		}

		// Check if it's a direct message event and not from us
		if event["type"] == "message" && event["user"] != result.Self.ID {
			channel, ok := event["channel"].(string)
			if !ok {
				log.Printf("\033[1;31mError getting channel for user %s\033[0m", email)
				continue
			}

			// Check if it's a direct message (channel starts with 'D')
			if !strings.HasPrefix(channel, "D") {
				continue
			}

			// Send "blah" as a response
			response := map[string]interface{}{
				"id":      1,
				"type":    "message",
				"channel": channel,
				"text":    "blah",
			}

			responseJSON, err := json.Marshal(response)
			if err != nil {
				log.Printf("\033[1;31mError creating response JSON for user %s: %v\033[0m", email, err)
				continue
			}

			if err := c.WriteMessage(websocket.TextMessage, responseJSON); err != nil {
				log.Printf("\033[1;31mError sending response for user %s: %v\033[0m", email, err)
				return
			}

			log.Printf("\033[1;32mSent 'blah' response for user %s in direct message %s\033[0m", email, channel)
		}
	}
}

func getRandomFullName() (string, error) {
	apiURL := "https://randommer.io/Name"
	data := map[string]string{
		"number": "1",
		"type":   "fullname",
	}

	formBody := []byte(fmt.Sprintf("number=%s&type=%s", data["number"], data["type"]))

	client := &http.Client{}
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(formBody))
	if err != nil {
		return "", err
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("X-Requested-With", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Log the response as is
	log.Printf("Response from randommer.io: %s", string(body))

	// Remove brackets and quotes from the response
	cleanedName := strings.Trim(string(body), "[]\"")

	return cleanedName, nil
}
