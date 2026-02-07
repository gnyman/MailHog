module github.com/gnyman/MailHog

go 1.25.5

require (
	github.com/gnyman/MailHog-Server v0.0.0
	github.com/gnyman/MailHog-UI v1.0.1
	github.com/gorilla/pat v1.0.2
	github.com/ian-kent/envconf v0.0.0-20141026121121-c19809918c02
	github.com/ian-kent/go-log v0.0.0-20160113211217-5731446c36ab
	github.com/mailhog/http v1.0.1
	github.com/mailhog/mhsendmail v0.2.0
	golang.org/x/crypto v0.47.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/gorilla/context v1.1.2 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/ian-kent/goose v0.0.0-20141221090059-c3541ea826ad // indirect
	github.com/kr/text v0.1.0 // indirect
	github.com/mailhog/data v1.0.1 // indirect
	github.com/mailhog/smtp v1.0.1 // indirect
	github.com/mailhog/storage v1.0.1 // indirect
	github.com/ogier/pflag v0.0.1 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/t-k/fluent-logger-golang v1.0.0 // indirect
	github.com/tinylib/msgp v1.6.3 // indirect
	gopkg.in/mgo.v2 v2.0.0-20190816093944-a6b53ec6cb22 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/gnyman/MailHog-Server => ./MailHog-Server
	github.com/gnyman/MailHog-UI => ./MailHog-UI
)
