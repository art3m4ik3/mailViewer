package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/gorilla/mux"
	"gopkg.in/gomail.v2"
)

type MailAccount struct {
	Username   string
	Password   string
	IMAPServer string
	IMAPPort   int
	SMTPServer string
	SMTPPort   int
}

type MailClient struct {
	Accounts       []MailAccount
	CurrentAccount *MailAccount
}

type Email struct {
	Subject string
	From    string
	Date    string
	Body    string
}

func NewMailClient() *MailClient {
	return &MailClient{
		Accounts: make([]MailAccount, 0),
	}
}

func (mc *MailClient) AddAccount(username, password, imapServer string, imapPort int, smtpServer string, smtpPort int) {
	account := MailAccount{
		Username:   username,
		Password:   password,
		IMAPServer: imapServer,
		IMAPPort:   imapPort,
		SMTPServer: smtpServer,
		SMTPPort:   smtpPort,
	}
	mc.Accounts = append(mc.Accounts, account)
	if mc.CurrentAccount == nil {
		mc.CurrentAccount = &mc.Accounts[0]
	}
}

func (mc *MailClient) SelectAccount(index int) error {
	if index < 0 || index >= len(mc.Accounts) {
		return fmt.Errorf("error index out of range")
	}
	mc.CurrentAccount = &mc.Accounts[index]
	return nil
}

func (mc *MailClient) FetchEmails() ([]Email, error) {
	if mc.CurrentAccount == nil {
		return nil, fmt.Errorf("account not selected")
	}

	imapAddr := fmt.Sprintf("%s:%d", mc.CurrentAccount.IMAPServer, mc.CurrentAccount.IMAPPort)

	c, err := client.DialTLS(imapAddr, nil)
	if err != nil {
		return nil, err
	}
	defer c.Logout()

	if err := c.Login(mc.CurrentAccount.Username, mc.CurrentAccount.Password); err != nil {
		return nil, err
	}

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		return nil, err
	}

	from := uint32(1)
	to := mbox.Messages
	if to > 10 {
		from = to - 9
	}
	seqSet := new(imap.SeqSet)
	seqSet.AddRange(from, to)

	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqSet, []imap.FetchItem{imap.FetchEnvelope, imap.FetchBody}, messages)
	}()

	var emails []Email
	for msg := range messages {
		email := Email{
			Subject: msg.Envelope.Subject,
			From:    msg.Envelope.From[0].PersonalName,
			Date:    msg.Envelope.Date.Format("2006-01-02 15:04:05"),
		}
		emails = append(emails, email)
	}

	if err := <-done; err != nil {
		return nil, err
	}

	return emails, nil
}

func (mc *MailClient) SendEmail(to, subject, body string) error {
	if mc.CurrentAccount == nil {
		return fmt.Errorf("account not selected")
	}

	m := gomail.NewMessage()
	m.SetHeader("From", mc.CurrentAccount.Username)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", body)

	d := gomail.NewDialer(mc.CurrentAccount.SMTPServer, mc.CurrentAccount.SMTPPort, mc.CurrentAccount.Username, mc.CurrentAccount.Password)

	if err := d.DialAndSend(m); err != nil {
		return err
	}

	return nil
}

func (mc *MailClient) SaveAccounts(filename string) error {
	data, err := json.Marshal(mc.Accounts)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filename, data, 0644)
}

func (mc *MailClient) LoadAccounts(filename string) error {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &mc.Accounts)
}

func main() {
	mailClient := NewMailClient()

	if err := mailClient.LoadAccounts("accounts.json"); err != nil {
		log.Println("Failed to load accounts:", err)
	}

	startWebServer(mailClient)
}

func startWebServer(mc *MailClient) {
	r := mux.NewRouter()

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if mc.CurrentAccount == nil && len(mc.Accounts) > 0 {
			mc.CurrentAccount = &mc.Accounts[0]
		}
		renderTemplate(w, "index.html", mc)
	})

	r.HandleFunc("/add_account", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			err := r.ParseForm()
			if err != nil {
				return
			}
			imapPort, _ := strconv.Atoi(r.Form.Get("imap_port"))
			smtpPort, _ := strconv.Atoi(r.Form.Get("smtp_port"))
			mc.AddAccount(
				r.Form.Get("username"),
				r.Form.Get("password"),
				r.Form.Get("imap_server"),
				imapPort,
				r.Form.Get("smtp_server"),
				smtpPort,
			)
			err = mc.SaveAccounts("accounts.json")
			if err != nil {
				return
			}
			http.Redirect(w, r, "/", http.StatusSeeOther)
		} else {
			renderTemplate(w, "add_account.html", nil)
		}
	})

	r.HandleFunc("/select_account/{id:[0-9]+}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id, _ := strconv.Atoi(vars["id"])
		err := mc.SelectAccount(id)
		if err != nil {
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	r.HandleFunc("/fetch_emails", func(w http.ResponseWriter, r *http.Request) {
		emails, err := mc.FetchEmails()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		renderTemplate(w, "emails.html", emails)
	})

	r.HandleFunc("/send_email", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			err := r.ParseForm()
			if err != nil {
				return
			}
			err = mc.SendEmail(
				r.Form.Get("to"),
				r.Form.Get("subject"),
				r.Form.Get("body"),
			)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/", http.StatusSeeOther)
		} else {
			renderTemplate(w, "send_email.html", nil)
		}
	})

	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	log.Println("Listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}

func renderTemplate(w http.ResponseWriter, tmpl string, data interface{}) {
	t, err := template.ParseFiles("templates/layout.html", "templates/"+tmpl)
	if err != nil {
		log.Printf("Error parsing template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = t.ExecuteTemplate(w, "layout", data)
	if err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
