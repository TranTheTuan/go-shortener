#!/usr/bin/env bash
#
# sitemap-to-csv.sh — crawl a site's sitemap(s) and emit every page URL as a CSV
# that matches the bulk-upload template (header: url,result).
#
# Usage:
#   ./sitemap-to-csv.sh https://devopscube.com/         # -> devopscube.com.csv
#   ./sitemap-to-csv.sh https://example.com out.csv     # -> out.csv
#
# How it finds URLs: robots.txt "Sitemap:" lines first, else common paths;
# then follows sitemap-index files recursively down to the actual <urlset> pages.
set -euo pipefail

UA="Mozilla/5.0 (compatible; sitemap-to-csv/1.0)"

[ $# -ge 1 ] || { echo "usage: $0 <site-url> [output.csv]" >&2; exit 1; }

site="$1"
[[ "$site" =~ ^https?:// ]] || site="https://$site"   # default to https
base="${site%/}"
domain="$(printf '%s' "$base" | sed -E 's,^https?://,,; s,/.*$,,')"
out="${2:-${domain}.csv}"
prefix="${out%.csv}"   # split output into ${prefix}-0.csv, -1.csv, ... (10k urls each)

# fetch a sitemap URL to stdout, transparently decompressing .gz.
fetch() {
  if [[ "$1" == *.gz ]]; then
    curl -fsSL -A "$UA" "$1" 2>/dev/null | gunzip -c 2>/dev/null || true
  else
    curl -fsSL --compressed -A "$UA" "$1" 2>/dev/null || true
  fi
}

# extract <loc> values from XML on stdin (|| true: no match is not fatal under set -e).
locs() { grep -oE '<loc>[^<]+</loc>' | sed -E 's,</?loc>,,g' || true; }

# extract page URLs from a plain-text sitemap (one URL per line).
txt_locs() { grep -oE 'https?://[^[:space:]]+' || true; }

tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT

# crawl a sitemap URL: recurse if it's an index, else collect its page URLs.
crawl() {
  local xml; xml="$(fetch "$1")"
  [ -n "$xml" ] || return 0
  # here-strings, not `printf | grep -q`: grep -q exits on first match and the
  # SIGPIPE'd printf would fail the pipeline under pipefail, misrouting big files.
  if grep -q '<sitemapindex' <<<"$xml"; then
    locs <<<"$xml" | while read -r child; do crawl "$child"; done
  elif grep -q '<urlset' <<<"$xml"; then
    locs <<<"$xml" >> "$tmp"
  else
    # plain-text sitemap (e.g. sitemap.txt): one URL per line.
    txt_locs <<<"$xml" >> "$tmp"
  fi
}

# 1. discover sitemap(s): robots.txt, else common fallback paths.
mapfile -t sitemaps < <(curl -fsSL -A "$UA" "$base/robots.txt" 2>/dev/null \
  | grep -iE '^[[:space:]]*sitemap:' | sed -E 's,^[[:space:]]*[Ss]itemap:[[:space:]]*,,' | tr -d '\r')

if [ "${#sitemaps[@]}" -eq 0 ]; then
  for p in sitemap.xml sitemap_index.xml wp-sitemap.xml sitemap-index.xml; do
    if curl -fsI -A "$UA" "$base/$p" >/dev/null 2>&1; then
      sitemaps+=("$base/$p"); break
    fi
  done
fi

[ "${#sitemaps[@]}" -gt 0 ] || { echo "no sitemap found for $domain" >&2; exit 2; }

for s in "${sitemaps[@]}"; do crawl "$s"; done

# drop non-article URLs: images, media, docs, feeds (keep only page/article links).
grep -ivE '\.(jpe?g|png|gif|webp|svg|bmp|ico|avif|tiff?|mp4|webm|mov|avi|mp3|wav|ogg|pdf|zip|gz|rss|xml|css|js)([?#].*)?$' "$tmp" > "$tmp.f" && mv "$tmp.f" "$tmp"

# 2. dedupe, then split into ${prefix}-N.csv chunks of 10k urls each (header per file).
count="$(sort -u "$tmp" | awk -v p="$prefix" '
  (NR-1) % 10000 == 0 { f = p "-" idx++ ".csv"; print "url,result" > f }
  { print $0 "," > f }
  END { print NR }')"
echo "wrote $count urls -> ${prefix}-{0..$(( (count>0?count-1:0) / 10000 ))}.csv" >&2
