# Security

Please report vulnerabilities privately, through GitHub's
[private vulnerability reporting](https://github.com/zbysir/lightron/security/advisories/new), not in a public issue.
You'll get an answer within a week. Fixes go into the latest release; there are no long-term branches yet.

Worth knowing about Lightron's design:

- A `Handler` is served on a loopback port that any local program can reach. Requests without the app's secret
  cookie are refused; the cookie is set on the windows' first load, through a one-time login URL handed to the
  browser over the DevTools pipe (never on a command line other programs can read).
- The DevTools protocol runs over a pipe (`--remote-debugging-pipe`), not a port: other programs can't drive the
  app's browser.
- Bound functions (`Options.Bind`) can be called by any page shown in the app's windows on the app's own origin.
  Pages from other origins (links to other sites) open in the default browser, not in the app.
- `--unsafely-treat-insecure-origin-as-secure` is set for the app's own `http://<Host>` only, so that it is a secure
  context (clipboard, notifications) without a certificate.
