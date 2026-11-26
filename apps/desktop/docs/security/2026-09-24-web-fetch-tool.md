# Security record — WebFetch reads a page from the user's machine, 24 September 2026

**Subject:** `WebFetch` sends one `https` GET to a URL the model names and the user approves, from
the user's machine and network, and returns the page as markdown. It is offered on every machine,
to every turn on a model that takes tools. It is the first tool that makes a request from the
sidecar to a host the model chose. The living model is [security-model.md](../security-model.md).

## What leaves

A GET of the approved URL, upgraded to https, with `User-Agent: Kubetail/<version>` and an `Accept`
that lets a server send markdown. Nothing else: no userinfo (a URL carrying one is refused by
name), no cookie (a transport has no jar), no `Authorization`, and no body. The request leaves
from the user's machine, inside their network, so the server learns the user's address. The
URL itself is what the model chose, and so is everything in it: a query string can carry
whatever the model read, which is why the user sees it whole before it goes.

## The request is the URL and the host

**Every fetch that would run is asked** (`TestWebFetchRefusesByName`). The request draws *Fetch
this page?*, the URL in mono through `VisibleText`, never folded, since the sidecar caps it at
8,192 bytes, then the host on a line of its own, since a long URL can hide which host it goes
to. Both come from the sidecar's action: the URL as it is requested, its host in ASCII through
`idna`, so a look-alike internationalized name reads as its `xn--` form, and the host drawn is
the one Go dials, never a second parse in the webview. A settled call reads `Fetch <url>`.

**The gate reads the URL by name alone.** `Approval` skips a URL `target` refuses — another
scheme, userinfo, a host `idna` rejects, one past 8,192 bytes, a host with no dot, a host whose
last label is a number (`127.1`, `0x7f.1`, which a resolver or a proxy may read as an address),
and a non-public IP literal — and `Run` answers why without touching the network. It asks about
every other URL alike, and resolves nothing before the user decides.

## No local or private address is dialled

The transport's dialer checks the address it is about to connect to, after DNS, on every connection
(`TestWebFetchRefusesNonPublicNames`), so a name that resolves to a private address is refused,
one that resolves differently the second time is judged by its second answer
(`TestWebFetchRefusesARebindingName`), and a redirect's host is checked when it is dialled.
`Public` refuses loopback, private, link-local, multicast and unspecified addresses, `0.0.0.0/8`,
carrier-grade NAT, `192.0.0.0/24`, benchmarking and `240.0.0.0/4`, and judges an IPv6 address
that carries an IPv4 one (NAT64, 6to4, IPv4-compatible) by the one it carries (`TestPublic`).
A name with one refused and one public address reaches the public one.

**The proxy is the one exception.** With a proxy configured (`HTTPS_PROXY`, read by
`httpproxy.FromEnvironment`; on macOS what the login shell exports), the sidecar dials the proxy,
which is usually on a private address itself, and the proxy resolves the name
(`TestWebFetchLetsTheProxyThrough`). `HTTP_PROXY` carries no fetch, since every fetch is https, so
its address is checked like any other (`TestWebFetchChecksTheHTTPProxysAddress`). The checks by
name still hold, IP literals and numeric hosts included; where a name lands is the proxy's to decide.

## Redirects

The transport follows none; `Run` does. A redirect to the approved host, or that host with or
without one leading `www.`, on the same port and over https, is followed without asking, five at
most, each hop checked by name as the first URL was (`TestWebFetchFollowsWwwAndReturnsOtherRedirects`).
Any other is returned to the model, which must call again, so the user approves the new host. A
redirect to http is refused rather than upgraded, since the upgrade would land on the same
redirect.

## What comes back

The page, as markdown for HTML and as it came for the other text types; anything else, PDFs
included, is refused. It is web text: data the prompt tells the model is never an instruction,
like cluster text. It is not redacted inline — it is not the user's secrets — and a page saved
past 30,000 bytes to the chat's results directory is redacted when `Read` returns it. Every
string the server supplies in the sidecar's own lines is capped: a URL at 8,192 bytes, a media
type at 100, a title at 200 characters on one line, and a status is its code and Go's text for
it, never the server's reason phrase (`TestWebFetchAnswersAStatusWithoutTheBody`). A failure to
reach the page names its kind and never the error's text.

A body past 8 MiB is refused unread. The HTML conversion runs on a goroutine the call can leave,
so a slow page never holds the turn past its bound.

## The dependency

`github.com/JohannesKaufmann/html-to-markdown/v2` (MIT, pure Go) converts the parsed page. It
runs only on a body already fetched and bounded, makes no request, and produces text the webview
never interprets as HTML.

## Residual

- **With a proxy, the proxy decides where a name lands.** A name that resolves to a private
  address behind it is fetched if the proxy fetches it.
- **A `NO_PROXY` entry naming the proxy itself** would let a fetch of the proxy's own address
  dial it directly, past the check.
- **A public name inside a VPN** resolves to a public address the user's network routes
  internally; the check sees a public address.
- **A same-host or `www.` hop is followed without asking**, to any path on that host.
- **A conversion left running after a cancel** holds the parse tree, which for an 8 MiB page is
  many times its size, until it finishes.
- **A URL built from cluster text that the user approves** is fetched; the prompt tells the model
  not to build one, and the request shows it whole.
