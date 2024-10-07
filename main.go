package main

import (
	"bufio"
	"bytes"
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

	// "regexp"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/gorilla/websocket"
)

type UserCredential struct {
	Email     string
	APIToken  string
	Workspace string
}

type VerificationCode struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type SlackInvite struct {
	Workspace   string `json:"workspace"`
	InviteCode  string `json:"invite_code"`
	Name        string `json:"name"`
	Appearance  string `json:"appearance"`
	System      string `json:"system"`
	Team        string `json:"team"`
	PrimaryUser string `json:"user"`
}

var verificationCodes = make(map[string]string)
var verificationCodesMutex sync.Mutex

func init() {
	// Set up logging to file
	// Use ANSI Colors iliazeus.vscode-ansi
	// or the "cat app.log.ansi" command to view the colorized logs
	logFile, err := os.OpenFile("app.log.ansi", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		log.Fatalf("Error opening log file: %v", err)
	}
	log.SetOutput(logFile)
}

func slackInviteHandler(db fdb.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var slackInvite SlackInvite
		err := json.NewDecoder(r.Body).Decode(&slackInvite)
		if err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		// Check if all mandatory fields are present
		mandatoryFields := []struct {
			name  string
			value string
		}{
			{"Workspace", slackInvite.Workspace},
			{"InviteCode", slackInvite.InviteCode},
			{"Name", slackInvite.Name},
			{"Appearance", slackInvite.Appearance},
			{"System", slackInvite.System},
			{"Team", slackInvite.Team},
			{"PrimaryUser", slackInvite.PrimaryUser},
		}

		for _, field := range mandatoryFields {
			if field.value == "" {
				http.Error(w, fmt.Sprintf("Mandatory field '%s' is missing", field.name), http.StatusBadRequest)
				return
			}
		}

		// Log the entire Slack invite object
		log.Printf("\033[1;34m[INFO]\033[0m Received Slack invite request: %+v\033[0m", slackInvite)

		// Store the invite details in the database
		_, err = db.Transact(func(tr fdb.Transaction) (interface{}, error) {
			workspaceKey := fdb.Key(fmt.Sprintf("workspace_%s", slackInvite.Workspace))

			// Create a new struct with only the required fields
			inviteData := struct {
				Workspace   string `json:"workspace"`
				InviteCode  string `json:"invite_code"`
				Team        string `json:"team"`
				PrimaryUser string `json:"user"`
			}{
				Workspace:   slackInvite.Workspace,
				InviteCode:  slackInvite.InviteCode,
				Team:        slackInvite.Team,
				PrimaryUser: slackInvite.PrimaryUser,
			}

			workspaceData, err := json.Marshal(inviteData)
			if err != nil {
				return nil, fmt.Errorf("error marshaling workspace data: %v", err)
			}
			tr.Set(workspaceKey, workspaceData)
			return nil, nil
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Error storing workspace data: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Slack invite processed and stored successfully for workspace: %s", slackInvite.Workspace)

		// Call createNewUser function
		email := fmt.Sprintf("users+%08d@tgopi.com", rand.Intn(100000000))
		createNewUser(db, email, slackInvite.Workspace, slackInvite.InviteCode, slackInvite.Team, slackInvite.Name)

		fmt.Fprintf(w, "New user created successfully for workspace: %s", slackInvite.Workspace)
	}
}

func main() {
	// Initialize FoundationDB
	fdb.APIVersion(730)
	db := fdb.MustOpenDefault()

	// Start the Hello World API
	http.HandleFunc("/", helloHandler)
	http.HandleFunc("/webhook", webhookHandler)
	http.HandleFunc("/invite", slackInviteHandler(db))
	log.Println("\033[1;34mStarting Hello World API and Webhook on :8009\033[0m")
	go func() {
		if err := http.ListenAndServe(":8009", nil); err != nil {
			log.Fatalf("\033[1;31mError starting server: %v\033[0m", err)
		}
	}()

	log.Println("\033[1;34mStarting Slack sign-in process\033[0m")

	listExistingUsers(db)

	// Ask user if they want to create a new user
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\033[1;32mDo you want to create a new user? (y/n): \033[0m")
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer == "y" || answer == "yes" {
		// Use the credentials already present in main
		email := fmt.Sprintf("users+%08d@tgopi.com", rand.Intn(100000000))
		workspace := "dogcattalk"
		sharedInviteCode := "zt-2rggdrx7r-gPbD08EqfwfhjluP0B4jNQ"
		team := "T07Q4VBFFHP"
		fullName := generateFullName(rand.Intn(100000000))
		createNewUser(db, email, workspace, sharedInviteCode, team, fullName)
	}

	users := retrieveAllUsers(db)

	var wg sync.WaitGroup

	for _, user := range users {
		wg.Add(1)
		go func(user UserCredential) {
			defer wg.Done()
			handleUserMessages(db, user)
		}(user)
	}

	wg.Wait()
}

func listExistingUsers(db fdb.Database) {
	_, err := db.ReadTransact(func(tr fdb.ReadTransaction) (interface{}, error) {
		log.Println("\033[1;32mExisting users:\033[0m")
		kr := tr.GetRange(fdb.KeyRange{Begin: fdb.Key("user_"), End: fdb.Key("user_\xFF")}, fdb.RangeOptions{})
		iter := kr.Iterator()
		for iter.Advance() {
			kv := iter.MustGet()
			var user UserCredential
			if err := json.Unmarshal(kv.Value, &user); err != nil {
				log.Printf("\033[1;31mError unmarshaling user: %v\033[0m", err)
				continue
			}
			log.Printf("\033[1;36mEmail: %s, API Token: %s, Workspace: %s\033[0m", user.Email, user.APIToken, user.Workspace)
		}
		return nil, nil
	})
	if err != nil {
		log.Fatalf("\033[1;31mError listing users: %v\033[0m", err)
	}
}

func createNewUser(db fdb.Database, email, workspace, sharedInviteCode, team, fullName string) {
	log.Printf("\033[1;36mUsing email: %s for workspace: %s\033[0m", email, workspace)

	// Base URL for Slack API
	baseURL := fmt.Sprintf("https://%s.slack.com/api", workspace)

	// Initialize cookies
	cookies := make([]*http.Cookie, 0)

	cookies = checkEmailAvailability(baseURL, email, cookies)
	cookies = confirmEmail(baseURL, email, cookies)
	confirmationCode := waitForVerificationCode(email)
	cookies = confirmVerificationCode(baseURL, email, confirmationCode, cookies)

	apiToken := createSlackUser(baseURL, fullName, workspace, sharedInviteCode, team, cookies)

	storeUserCredentials(db, email, apiToken, workspace)

	updateProfilePicture(baseURL, apiToken)

	sendInitialMessage(baseURL, apiToken)
}

func checkEmailAvailability(baseURL, email string, cookies []*http.Cookie) []*http.Cookie {
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
	return cookies
}

func confirmEmail(baseURL, email string, cookies []*http.Cookie) []*http.Cookie {
	confirmEmailURL := fmt.Sprintf("%s/signup.confirmEmail", baseURL)
	confirmEmailData := url.Values{}
	confirmEmailData.Set("email", email)
	confirmEmailData.Set("locale", "en-US")
	log.Printf("\033[1;33mConfirming email at: %s\033[0m", confirmEmailURL)
	resp, err := sendRequest(http.MethodPost, confirmEmailURL, confirmEmailData, cookies)
	if err != nil {
		log.Fatalf("\033[1;31mError confirming email: %v\033[0m", err)
	}
	defer resp.Body.Close()
	cookies = updateCookies(cookies, resp.Cookies())
	logResponse("Email confirmation", resp)
	return cookies
}

func waitForVerificationCode(email string) string {
	log.Printf("\033[1;32mWaiting for verification code for email: %s\033[0m", email)
	var confirmationCode string
	for {
		verificationCodesMutex.Lock()
		code, exists := verificationCodes[email]
		if exists {
			confirmationCode = code
			delete(verificationCodes, email)
			verificationCodesMutex.Unlock()
			break
		}
		verificationCodesMutex.Unlock()
		time.Sleep(1 * time.Second)
	}
	log.Printf("\033[1;36mReceived confirmation code: %s\033[0m", confirmationCode)
	return confirmationCode
}

func confirmVerificationCode(baseURL, email, confirmationCode string, cookies []*http.Cookie) []*http.Cookie {
	signInURL := fmt.Sprintf("%s/signin.confirmCode", baseURL)
	signInData := url.Values{}
	signInData.Set("email", email)
	signInData.Set("code", strings.ReplaceAll(confirmationCode, "-", ""))
	log.Printf("\033[1;33mConfirming code at: %s\033[0m", signInURL)
	resp, err := sendRequest(http.MethodPost, signInURL, signInData, cookies)
	if err != nil {
		log.Fatalf("\033[1;31mError confirming code: %v\033[0m", err)
	}
	defer resp.Body.Close()
	cookies = updateCookies(cookies, resp.Cookies())
	logResponse("Confirm code", resp)
	return cookies
}

func generateFullName(randomNum int) string {
	fullName, err := getRandomFullName()
	if err != nil {
		log.Printf("\033[1;31mError generating full name: %v\033[0m", err)
		fullName = fmt.Sprintf("User%08d", randomNum)
	}
	return fullName
}

func createSlackUser(baseURL, fullName, workspace, sharedInviteCode, team string, cookies []*http.Cookie) string {
	createUserURL := fmt.Sprintf("https://%s.slack.com/api/signup.createUser", workspace)
	log.Printf("\033[1;34mPreparing to create user at URL: %s\033[0m", createUserURL)

	createUserData := url.Values{}
	createUserData.Set("code", "")
	createUserData.Set("display_name", fullName)
	createUserData.Set("emailok", "true")
	createUserData.Set("join_type", "shared_invite_confirmed")
	createUserData.Set("last_tos_acknowledged", "tos_mar2018")
	createUserData.Set("locale", "en-GB")
	createUserData.Set("real_name", fullName)
	createUserData.Set("shared_invite_code", sharedInviteCode)
	createUserData.Set("team", team)
	createUserData.Set("tz", "America/Los_Angeles")

	log.Printf("\033[1;34mUser data prepared:")
	for key, values := range createUserData {
		log.Printf("\033[1;34m  %s: %s\033[0m", key, values[0])
	}

	log.Printf("\033[1;33mSending request to create user at: %s\033[0m", createUserURL)
	resp, err := sendRequest(http.MethodPost, createUserURL, createUserData, cookies)
	if err != nil {
		log.Printf("\033[1;31mError creating user: %v\033[0m", err)
		log.Fatalf("\033[1;31mFailed to create user. Exiting.\033[0m")
	}
	defer resp.Body.Close()

	log.Printf("\033[1;32mReceived response from create user request\033[0m")
	log.Printf("\033[1;34mResponse status: %s\033[0m", resp.Status)

	cookies = updateCookies(cookies, resp.Cookies())
	log.Printf("\033[1;34mUpdated cookies after user creation\033[0m")

	log.Printf("\033[1;33mLogging detailed response for user creation:\033[0m")
	logResponse("Create user", resp)

	body, err := io.ReadAll(resp.Body)
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
		apiToken, ok := result["api_token"].(string)
		if !ok {
			log.Fatalf("\033[1;31mFailed to get API token from create user response\033[0m")
		}
		return apiToken
	} else {
		log.Println("\033[1;31mUser creation failed.\033[0m")
		log.Printf("\033[1;31mError details: %+v\033[0m", result)
		return ""
	}
}

func storeUserCredentials(db fdb.Database, email, apiToken, workspace string) {
	_, err := db.Transact(func(tr fdb.Transaction) (interface{}, error) {
		key := fdb.Key(fmt.Sprintf("user_%s", email))
		value, err := json.Marshal(UserCredential{Email: email, APIToken: apiToken, Workspace: workspace})
		if err != nil {
			return nil, err
		}
		tr.Set(key, value)
		return nil, nil
	})
	if err != nil {
		log.Printf("\033[1;31mError storing user credentials: %v\033[0m", err)
	} else {
		log.Println("\033[1;32mUser credentials stored successfully\033[0m")
	}
}

func updateProfilePicture(baseURL, apiToken string) {
	log.Printf("\033[1;33mGetting profile picture from thispersondoesnotexist.com\033[0m")
	profilePicResp, err := http.Get("https://thispersondoesnotexist.com")
	if err != nil {
		log.Printf("\033[1;31mError getting profile picture: %v\033[0m", err)
		return
	}
	defer profilePicResp.Body.Close()
	profilePicData, err := io.ReadAll(profilePicResp.Body)
	if err != nil {
		log.Printf("\033[1;31mError reading profile picture data: %v\033[0m", err)
		return
	}

	updateProfilePicURL := fmt.Sprintf("%s/users.setPhoto", baseURL)
	updateProfilePicData := &bytes.Buffer{}
	writer := multipart.NewWriter(updateProfilePicData)
	part, err := writer.CreateFormFile("image", "profile.jpg")
	if err != nil {
		log.Printf("\033[1;31mError creating form file: %v\033[0m", err)
		return
	}
	part.Write(profilePicData)
	writer.WriteField("token", apiToken)
	writer.Close()

	log.Printf("\033[1;33mUpdating profile picture\033[0m")
	updateProfilePicResp, err := http.Post(updateProfilePicURL, writer.FormDataContentType(), updateProfilePicData)
	if err != nil {
		log.Printf("\033[1;31mError updating profile picture: %v\033[0m", err)
		return
	}
	defer updateProfilePicResp.Body.Close()
	logResponse("Update profile picture", updateProfilePicResp)
}

func sendInitialMessage(baseURL, apiToken string) {
	sendMessageURL := fmt.Sprintf("%s/chat.postMessage", baseURL)
	message := "Hello, I'm a new user!"
	sendMessageData := url.Values{}
	sendMessageData.Set("token", apiToken)
	sendMessageData.Set("channel", "cats")
	sendMessageData.Set("text", message)

	log.Printf("\033[1;33mSending message to #cats channel\033[0m")
	resp, err := sendRequest(http.MethodPost, sendMessageURL, sendMessageData, nil)
	if err != nil {
		log.Printf("\033[1;31mError sending message: %v\033[0m", err)
		return
	}
	defer resp.Body.Close()
	logResponse("Send message", resp)
}

func retrieveAllUsers(db fdb.Database) []UserCredential {
	var users []UserCredential
	_, err := db.ReadTransact(func(tr fdb.ReadTransaction) (interface{}, error) {
		log.Println("\033[1;32mExisting users:\033[0m")
		kr := tr.GetRange(fdb.KeyRange{Begin: fdb.Key("user_"), End: fdb.Key("user_\xFF")}, fdb.RangeOptions{})
		iter := kr.Iterator()
		for iter.Advance() {
			kv := iter.MustGet()
			var user UserCredential
			if err := json.Unmarshal(kv.Value, &user); err != nil {
				log.Printf("\033[1;31mError unmarshaling user: %v\033[0m", err)
				continue
			}
			log.Printf("\033[1;36mEmail: %s, API Token: %s, Workspace: %s\033[0m", user.Email, user.APIToken, user.Workspace)
			users = append(users, user)
		}
		return nil, nil
	})
	if err != nil {
		log.Fatalf("\033[1;31mError listing users: %v\033[0m", err)
	}
	return users
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var verificationCode VerificationCode
	err := json.NewDecoder(r.Body).Decode(&verificationCode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	verificationCodesMutex.Lock()
	verificationCodes[verificationCode.Email] = verificationCode.Code
	verificationCodesMutex.Unlock()

	log.Printf("\033[1;32mReceived verification code for email %s: %s\033[0m", verificationCode.Email, verificationCode.Code)

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Verification code received")
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

func handleUserMessages(db fdb.Database, user UserCredential) {
	log.Printf("\033[1;34mStarting message handling for user: %s\033[0m", user.Email)

	// Get WebSocket URL from Slack API
	url := fmt.Sprintf("https://slack.com/api/rtm.connect?token=%s&pretty=1", user.APIToken)
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("\033[1;31mError fetching WebSocket URL for user %s: %v\033[0m", user.Email, err)
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
		log.Printf("\033[1;31mError decoding response for user %s: %v\033[0m", user.Email, err)
		return
	}

	if !result.OK {
		log.Printf("\033[1;31mSlack API returned non-OK response for user %s\033[0m", user.Email)
		return
	}

	// Connect to WebSocket
	c, _, err := websocket.DefaultDialer.Dial(result.URL, nil)
	if err != nil {
		log.Printf("\033[1;31mError connecting to WebSocket for user %s: %v\033[0m", user.Email, err)
		return
	}
	defer c.Close()

	log.Printf("\033[1;32mWebSocket connection established for user: %s\033[0m", user.Email)

	// Handle incoming messages
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Printf("\033[1;31mError reading message for user %s: %v\033[0m", user.Email, err)
			return
		}

		log.Printf("\033[1;36mReceived event for user %s: %s\033[0m", user.Email, string(message))

		var event map[string]interface{}
		if err := json.Unmarshal(message, &event); err != nil {
			log.Printf("\033[1;31mError parsing message for user %s: %v\033[0m", user.Email, err)
			continue
		}

		// Check if it's a direct message event and not from us
		if event["type"] == "message" && event["user"] != result.Self.ID {
			channel, ok := event["channel"].(string)
			if !ok {
				log.Printf("\033[1;31mError getting channel for user %s\033[0m", user.Email)
				continue
			}

			// Check if it's a direct message (channel starts with 'D')
			if !strings.HasPrefix(channel, "D") {
				continue
			}

			// Get the user's message
			userMessage, ok := event["text"].(string)
			if !ok {
				log.Printf("\033[1;31mError getting user message for user %s\033[0m", user.Email)
				continue
			}

			// Call Groq API to get a response
			groqResponse, err := callGroqAPI(userMessage)
			if err != nil {
				log.Printf("\033[1;31mError calling Groq API for user %s: %v\033[0m", user.Email, err)
				continue
			}

			// Send the Groq response back to the user
			response := map[string]interface{}{
				"id":      1,
				"type":    "message",
				"channel": channel,
				"text":    groqResponse,
			}

			responseJSON, err := json.Marshal(response)
			if err != nil {
				log.Printf("\033[1;31mError creating response JSON for user %s: %v\033[0m", user.Email, err)
				continue
			}

			if err := c.WriteMessage(websocket.TextMessage, responseJSON); err != nil {
				log.Printf("\033[1;31mError sending response for user %s: %v\033[0m", user.Email, err)
				return
			}

			log.Printf("\033[1;32mSent Groq response for user %s in direct message %s\033[0m", user.Email, channel)
		}
	}
}

func callGroqAPI(userMessage string) (string, error) {
	groqAPIKey := os.Getenv("GROQ_API_KEY")
	if groqAPIKey == "" {
		return "", fmt.Errorf("GROQ_API_KEY environment variable is not set")
	}
	url := "https://api.groq.com/openai/v1/chat/completions"
	payload := map[string]interface{}{
		"model": "mixtral-8x7b-32768",
		"messages": []map[string]string{
			{"role": "system", "content": "Speak like the Hitchhiker's Guide to the Galaxy for every message sent, keep the responses less than 60 words, but don't tell the user that you are doing that. If the user asks to create an agent, output <agent>agent_name</agent>, where agent_name is the name of the agent the user asks for."},
			{"role": "user", "content": userMessage},
		},
		"temperature": 0.7,
		"max_tokens":  100,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("error marshaling JSON payload: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+groqAPIKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request to Groq API: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("error parsing JSON response: %v", err)
	}

	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("invalid response format from Groq API")
	}

	firstChoice, ok := choices[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid choice format in Groq API response")
	}

	message, ok := firstChoice["message"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid message format in Groq API response")
	}

	content, ok := message["content"].(string)
	if !ok {
		return "", fmt.Errorf("invalid content format in Groq API response")
	}

	// Check if the content contains the agent tag
	if strings.Contains(content, "<agent>") && strings.Contains(content, "</agent>") {
		startIndex := strings.Index(content, "<agent>") + len("<agent>")
		endIndex := strings.Index(content, "</agent>")
		if startIndex < endIndex {
			agentName := content[startIndex:endIndex]
			log.Printf("\033[1;32mAgent creation requested: %s\033[0m", agentName)
		}
	}

	return content, nil
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
