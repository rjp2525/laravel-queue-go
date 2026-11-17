# Documentation site

The source of [laravel-queue-go.reno.sh](https://laravel-queue-go.reno.sh).

It is a small static site: Markdown in `content/`, rendered by the Go program
in `main.go` into `dist/`. It lives in its own module so its dependencies stay
out of the library's `go.mod`.

```bash
cd website
go run . serve          # build, then serve on http://127.0.0.1:8080
go run . build          # build only, into ./dist
```

## Editing

- **A page:** edit the Markdown file under `content/docs/`. The front matter
  needs a `title` and a `description`.
- **A new page:** add the file, then add its name to the `nav` list at the top
  of `main.go`. That list is the sidebar, in order.
- **Callouts:** a blockquote starting with `[!NOTE]`, `[!TIP]` or `[!WARNING]`.
- **Styles:** `static/css/site.css`. The look follows reno.sh: Geist Mono, a
  framed window, navy rules, teal and orange accents.

The build checks every internal link and heading anchor and fails if one is
broken, so renaming a page or heading can't leave dead links behind.

## Deploying

`.github/workflows/docs.yml` builds the site on every push to `main` that
touches `website/` and deploys it to GitHub Pages. Pull requests get the build
(and the link check) without the deploy.

One-time setup in the repository settings, under **Pages**:

1. Source: **GitHub Actions**.
2. Custom domain: `laravel-queue-go.reno.sh`, then enable **Enforce HTTPS**
   once the certificate is issued.

And one DNS record at the provider for `reno.sh`:

```text
laravel-queue-go   CNAME   rjp2525.github.io.
```
