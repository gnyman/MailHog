package api

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/pat"
	"github.com/ian-kent/go-log/log"
	"github.com/gnyman/MailBoar-Server/config"
	"github.com/mailhog/data"
	"github.com/mailhog/storage"

	"github.com/ian-kent/goose"
)

// APIv1 implements version 1 of the MailBoar API
//
// The specification has been frozen and will eventually be deprecated.
// Only bug fixes and non-breaking changes will be applied here.
//
// Any changes/additions should be added in APIv2.
type APIv1 struct {
	config       *config.Config
	messageChan  chan *data.Message
	sentMessages map[string]bool
}

// FIXME should probably move this into APIv1 struct
var stream *goose.EventStream

// ReleaseConfig is an alias to preserve go package API
type ReleaseConfig config.OutgoingSMTP

func createAPIv1(conf *config.Config, r *pat.Router) *APIv1 {
	log.Println("Creating API v1 with WebPath: " + conf.WebPath)
	apiv1 := &APIv1{
		config:       conf,
		messageChan:  make(chan *data.Message),
		sentMessages: make(map[string]bool),
	}

	stream = goose.NewEventStream()

	r.Path(conf.WebPath + "/api/v1/messages").Methods("GET").HandlerFunc(apiv1.messages)
	r.Path(conf.WebPath + "/api/v1/messages").Methods("DELETE").HandlerFunc(apiv1.delete_all)
	r.Path(conf.WebPath + "/api/v1/messages").Methods("OPTIONS").HandlerFunc(apiv1.defaultOptions)

	r.Path(conf.WebPath + "/api/v1/messages/{id}").Methods("GET").HandlerFunc(apiv1.message)
	r.Path(conf.WebPath + "/api/v1/messages/{id}").Methods("DELETE").HandlerFunc(apiv1.delete_one)
	r.Path(conf.WebPath + "/api/v1/messages/{id}").Methods("OPTIONS").HandlerFunc(apiv1.defaultOptions)

	r.Path(conf.WebPath + "/api/v1/messages/{id}/download").Methods("GET").HandlerFunc(apiv1.download)
	r.Path(conf.WebPath + "/api/v1/messages/{id}/download").Methods("OPTIONS").HandlerFunc(apiv1.defaultOptions)

	r.Path(conf.WebPath + "/api/v1/messages/{id}/mime/part/{part}/download").Methods("GET").HandlerFunc(apiv1.download_part)
	r.Path(conf.WebPath + "/api/v1/messages/{id}/mime/part/{part}/download").Methods("OPTIONS").HandlerFunc(apiv1.defaultOptions)

	r.Path(conf.WebPath + "/api/v1/messages/{id}/release").Methods("POST").HandlerFunc(apiv1.release_one)
	r.Path(conf.WebPath + "/api/v1/messages/{id}/release").Methods("OPTIONS").HandlerFunc(apiv1.defaultOptions)

	r.Path(conf.WebPath + "/api/v1/sent").Methods("GET").HandlerFunc(apiv1.sent)
	r.Path(conf.WebPath + "/api/v1/sent").Methods("OPTIONS").HandlerFunc(apiv1.defaultOptions)

	r.Path(conf.WebPath + "/api/v1/events").Methods("GET").HandlerFunc(apiv1.eventstream)
	r.Path(conf.WebPath + "/api/v1/events").Methods("OPTIONS").HandlerFunc(apiv1.defaultOptions)

	go func() {
		keepaliveTicker := time.Tick(time.Minute)
		for {
			select {
			case msg := <-apiv1.messageChan:
				log.Println("Got message in APIv1 event stream")
				bytes, _ := json.MarshalIndent(msg, "", "  ")
				json := string(bytes)
				log.Printf("Sending content: %s\n", json)
				apiv1.broadcast(json)
			case <-keepaliveTicker:
				apiv1.keepalive()
			}
		}
	}()

	return apiv1
}

func (apiv1 *APIv1) defaultOptions(w http.ResponseWriter, req *http.Request) {
	if len(apiv1.config.CORSOrigin) > 0 {
		w.Header().Add("Access-Control-Allow-Origin", apiv1.config.CORSOrigin)
		w.Header().Add("Access-Control-Allow-Methods", "OPTIONS,GET,POST,DELETE")
		w.Header().Add("Access-Control-Allow-Headers", "Content-Type")
	}
}

func (apiv1 *APIv1) broadcast(json string) {
	log.Println("[APIv1] BROADCAST /api/v1/events")
	b := []byte(json)
	stream.Notify("data", b)
}

// keepalive sends an empty keep alive message.
//
// This not only can keep connections alive, but also will detect broken
// connections. Without this it is possible for the server to become
// unresponsive due to too many open files.
func (apiv1 *APIv1) keepalive() {
	log.Println("[APIv1] KEEPALIVE /api/v1/events")
	stream.Notify("keepalive", []byte{})
}

func (apiv1 *APIv1) sent(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv1] GET /api/v1/sent")

	apiv1.defaultOptions(w, req)

	ids := make([]string, 0, len(apiv1.sentMessages))
	for id := range apiv1.sentMessages {
		ids = append(ids, id)
	}

	bytes, _ := json.Marshal(ids)
	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
}

func (apiv1 *APIv1) eventstream(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv1] GET /api/v1/events")

	//apiv1.defaultOptions(session)
	if len(apiv1.config.CORSOrigin) > 0 {
		w.Header().Add("Access-Control-Allow-Origin", apiv1.config.CORSOrigin)
		w.Header().Add("Access-Control-Allow-Methods", "OPTIONS,GET,POST,DELETE")
	}

	stream.AddReceiver(w)
}

func (apiv1 *APIv1) messages(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv1] GET /api/v1/messages")

	apiv1.defaultOptions(w, req)

	// TODO start, limit
	switch apiv1.config.Storage.(type) {
	case *storage.MongoDB:
		messages, _ := apiv1.config.Storage.(*storage.MongoDB).List(0, 1000)
		bytes, _ := json.Marshal(messages)
		w.Header().Add("Content-Type", "text/json")
		w.Write(bytes)
	case *storage.InMemory:
		messages, _ := apiv1.config.Storage.(*storage.InMemory).List(0, 1000)
		bytes, _ := json.Marshal(messages)
		w.Header().Add("Content-Type", "text/json")
		w.Write(bytes)
	default:
		w.WriteHeader(500)
	}
}

func (apiv1 *APIv1) message(w http.ResponseWriter, req *http.Request) {
	id := req.URL.Query().Get(":id")
	log.Printf("[APIv1] GET /api/v1/messages/%s\n", id)

	apiv1.defaultOptions(w, req)

	message, err := apiv1.config.Storage.Load(id)
	if err != nil {
		log.Printf("- Error: %s", err)
		w.WriteHeader(500)
		return
	}

	bytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("- Error: %s", err)
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "text/json")
	w.Write(bytes)
}

func (apiv1 *APIv1) download(w http.ResponseWriter, req *http.Request) {
	id := req.URL.Query().Get(":id")
	log.Printf("[APIv1] GET /api/v1/messages/%s\n", id)

	apiv1.defaultOptions(w, req)

	w.Header().Set("Content-Type", "message/rfc822")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+id+".eml\"")

	switch apiv1.config.Storage.(type) {
	case *storage.MongoDB:
		message, _ := apiv1.config.Storage.(*storage.MongoDB).Load(id)
		for h, l := range message.Content.Headers {
			for _, v := range l {
				w.Write([]byte(h + ": " + v + "\r\n"))
			}
		}
		w.Write([]byte("\r\n" + message.Content.Body))
	case *storage.InMemory:
		message, _ := apiv1.config.Storage.(*storage.InMemory).Load(id)
		for h, l := range message.Content.Headers {
			for _, v := range l {
				w.Write([]byte(h + ": " + v + "\r\n"))
			}
		}
		w.Write([]byte("\r\n" + message.Content.Body))
	default:
		w.WriteHeader(500)
	}
}

func (apiv1 *APIv1) download_part(w http.ResponseWriter, req *http.Request) {
	id := req.URL.Query().Get(":id")
	part := req.URL.Query().Get(":part")
	log.Printf("[APIv1] GET /api/v1/messages/%s/mime/part/%s/download\n", id, part)

	// TODO extension from content-type?
	apiv1.defaultOptions(w, req)

	w.Header().Set("Content-Disposition", "attachment; filename=\""+id+"-part-"+part+"\"")

	message, _ := apiv1.config.Storage.Load(id)
	contentTransferEncoding := ""
	pid, _ := strconv.Atoi(part)
	for h, l := range message.MIME.Parts[pid].Headers {
		for _, v := range l {
			switch strings.ToLower(h) {
			case "content-disposition":
				// Prevent duplicate "content-disposition"
				w.Header().Set(h, v)
			case "content-transfer-encoding":
				if contentTransferEncoding == "" {
					contentTransferEncoding = v
				}
				fallthrough
			default:
				w.Header().Add(h, v)
			}
		}
	}
	body := []byte(message.MIME.Parts[pid].Body)
	if strings.ToLower(contentTransferEncoding) == "base64" {
		var e error
		body, e = base64.StdEncoding.DecodeString(message.MIME.Parts[pid].Body)
		if e != nil {
			log.Printf("[APIv1] Decoding base64 encoded body failed: %s", e)
		}
	}
	w.Write(body)
}

func (apiv1 *APIv1) delete_all(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv1] POST /api/v1/messages")

	apiv1.defaultOptions(w, req)

	w.Header().Add("Content-Type", "text/json")

	err := apiv1.config.Storage.DeleteAll()
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}

	apiv1.sentMessages = make(map[string]bool)
	w.WriteHeader(200)
}

func (apiv1 *APIv1) release_one(w http.ResponseWriter, req *http.Request) {
	id := req.URL.Query().Get(":id")
	log.Printf("[APIv1] POST /api/v1/messages/%s/release\n", id)

	apiv1.defaultOptions(w, req)

	w.Header().Add("Content-Type", "text/json")
	msg, err := apiv1.config.Storage.Load(id)
	if err != nil {
		log.Printf("Failed to load message: %s", err)
		w.WriteHeader(500)
		w.Write([]byte("Failed to load message"))
		return
	}

	log.Printf("Got message: %s", msg.ID)

	// Use original envelope sender and recipients from the captured message
	from := msg.Raw.From
	to := msg.Raw.To
	helo := msg.Raw.Helo

	if len(from) == 0 {
		from = "nobody@" + apiv1.config.Hostname
	}
	if len(helo) == 0 {
		helo = apiv1.config.Hostname
	}

	// Reconstruct the email message from headers + body
	msgBytes := make([]byte, 0)
	for h, l := range msg.Content.Headers {
		for _, v := range l {
			msgBytes = append(msgBytes, []byte(h+": "+v+"\r\n")...)
		}
	}
	msgBytes = append(msgBytes, []byte("\r\n"+msg.Content.Body)...)

	addr := apiv1.config.ReleaseSMTPAddr
	log.Printf("Releasing to %s (from: %s, helo: %s, via %s)", strings.Join(to, ", "), from, helo, addr)

	// Use manual SMTP client to control EHLO and STARTTLS behavior
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Printf("Failed to connect to SMTP server: %s", err)
		w.WriteHeader(500)
		w.Write([]byte(fmt.Sprintf("Failed to connect to SMTP server: %s", err)))
		return
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		log.Printf("Failed to create SMTP client: %s", err)
		w.WriteHeader(500)
		return
	}
	defer c.Close()

	// Send EHLO with the original HELO hostname
	if err = c.Hello(helo); err != nil {
		log.Printf("EHLO failed: %s", err)
		w.WriteHeader(500)
		return
	}

	// Only use STARTTLS if configured
	if apiv1.config.ReleaseStartTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			tlsConfig := &tls.Config{ServerName: host}
			if err = c.StartTLS(tlsConfig); err != nil {
				log.Printf("STARTTLS failed: %s", err)
				w.WriteHeader(500)
				return
			}
		}
	}

	// Set envelope sender (original MAIL FROM)
	if err = c.Mail(from); err != nil {
		log.Printf("MAIL FROM failed: %s", err)
		w.WriteHeader(500)
		return
	}

	// Set envelope recipients (original RCPT TO)
	for _, rcpt := range to {
		if err = c.Rcpt(rcpt); err != nil {
			log.Printf("RCPT TO failed for %s: %s", rcpt, err)
			w.WriteHeader(500)
			return
		}
	}

	// Send the message data
	wc, err := c.Data()
	if err != nil {
		log.Printf("DATA failed: %s", err)
		w.WriteHeader(500)
		return
	}

	_, err = wc.Write(msgBytes)
	if err != nil {
		log.Printf("Failed to write message data: %s", err)
		w.WriteHeader(500)
		return
	}

	err = wc.Close()
	if err != nil {
		log.Printf("Failed to close data writer: %s", err)
		w.WriteHeader(500)
		return
	}

	c.Quit()
	apiv1.sentMessages[id] = true
	log.Printf("Message released successfully")
}

func (apiv1 *APIv1) delete_one(w http.ResponseWriter, req *http.Request) {
	id := req.URL.Query().Get(":id")

	log.Printf("[APIv1] POST /api/v1/messages/%s/delete\n", id)

	apiv1.defaultOptions(w, req)

	w.Header().Add("Content-Type", "text/json")
	err := apiv1.config.Storage.DeleteOne(id)
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}
	delete(apiv1.sentMessages, id)
	w.WriteHeader(200)
}
