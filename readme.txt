to setup:
- setup foundationdb using the install_fdb_go script, and random info below
- setup caddy to serve the go server (go run main.go) with https://<server> which provides the main page as well as the /webhook endpoint
- setup cloudflare email worker (worker.js) to call a https://<server>/webhook endpoint with the verification code

----- random info below -----

go run main.go

todo?
- debug page/portal, when online, etc. which messages did they not respond to, and why?
- ask users to change their behavior, otherwise ask PM to replace them
- pay per active user, so limited budget.
- send emails to user+<name>@slack.adi.fr.eu.org using cloudflare email workers to a webhook.
- use fal for ultra fast image generation https://fal.ai/pricing

debug portal to have the system messages for debugging as well.

- Setup caddy server to run the email worker, doesn't work directly.

guide:
- https://github.com/apple/foundationdb/releases/tag/7.3.26 install
- maybe just run it on the cloud directly, simpler fdb usage.
- actually try installing the language bindings from the package source. release
./install_fdb_go  install --fdbver 7.3.26      


The FoundationDB go bindings were successfully installed.
To build packages which use the go bindings, you will need to
set the following environment variables:
   CGO_CPPFLAGS="-I/home/a/go/src/github.com/apple/foundationdb/bindings/c"
   CGO_CFLAGS="-g -O2"
   CGO_LDFLAGS="-L/usr/lib"


cp /usr/include/foundationdb/fdb_c_apiversion.g.h /home/a/go/src/github.com/apple/foundationdb/bindings/c/foundationdb/
cp /usr/include/foundationdb/fdb_c_options.g.h /home/a/go/src/github.com/apple/foundationdb/bindings/c/foundationdb/