#!/usr/bin/env python3
"""Fill a running GoatCounter instance with generated pageviews through /api.

All statistics are derived from the ``hits`` table, exactly as cron/*_stat.go
does it, so the dashboard sees the same thing it would after collecting this
traffic for real. Statistics count visits rather than pageviews: a pageview is
only counted the first time a visit sees that path.

WARNING: this deletes all existing pageviews and statistics.
"""

import argparse
import json
import os
import random
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone


SITES = ("example.com", "foobar.net")
PATHS = (
    ("/", False, 22),
    ("/blog", False, 10),
    ("/blog/hello-world", False, 14),
    ("/blog/self-hosting-goatcounter", False, 16),
    ("/blog/why-sqlite", False, 11),
    ("/blog/privacy-first-analytics", False, 9),
    ("/docs", False, 6),
    ("/docs/installation", False, 7),
    ("/docs/configuration", False, 5),
    ("/about", False, 4),
    ("/contact", False, 3),
    ("download-brochure", True, 0),
    ("signup-click", True, 0),
    ("newsletter-subscribe", True, 0),
)
REFERRERS = (
    ("", "o", 100),
    ("www.google.com", "h", 38),
    ("news.ycombinator.com", "h", 17),
    ("duckduckgo.com", "h", 9),
    ("github.com", "h", 8),
    ("www.reddit.com", "h", 7),
    ("lobste.rs", "h", 6),
    ("x.com", "h", 5),
    ("mastodon.social", "h", 4),
    ("en.wikipedia.org", "h", 3),
)
CAMPAIGNS = (
    ("spring-launch", "newsletter", 10),
    ("spring-launch", "x", 6),
    ("docs-push", "newsletter", 5),
    ("conf-talk", "slides", 4),
)
DEVICES = (
    ("Chrome", "141", "Windows", "11", (1920, 1536, 1366, 2560), 26),
    ("Chrome", "140", "Windows", "10", (1920, 1366), 8),
    ("Chrome", "141", "macOS", "15.6", (1512, 1728, 2560), 7),
    ("Chrome", "141", "Android", "15", (360, 393, 412, 800), 13),
    ("Firefox", "143", "Linux", "", (1920, 2560, 1280), 6),
    ("Firefox", "143", "Windows", "11", (1920, 1366), 5),
    ("Safari", "18.6", "macOS", "15.6", (1512, 1728, 1440), 11),
    ("Safari", "18.6", "iOS", "18.6", (390, 414, 430, 768, 820, 1024), 14),
    ("Edge", "141", "Windows", "11", (1920, 1536), 7),
    ("Samsung Internet", "28", "Android", "14", (360, 412), 3),
)
LOCATIONS = (
    ("DE", 30), ("US", 10), ("US-CA", 9), ("US-NY", 7),
    ("GB", 9), ("NL", 7), ("AT", 6), ("CH", 5), ("FR", 5),
    ("PL", 4), ("CA", 4), ("IN", 4), ("SE", 3), ("BR", 3),
    ("JP", 2), ("AU", 2),
)
LANGUAGES = (
    ("deu", 30), ("eng", 26), ("nld", 8), ("fra", 6), ("pol", 5),
    ("spa", 4), ("por", 3), ("swe", 3), ("ita", 3), ("ces", 2),
    ("jpn", 2), ("hin", 2), ("zho", 2), ("rus", 2), ("", 3),
)
HOUR_WEIGHTS = (2, 1, 1, 1, 1, 2, 4, 7, 11, 15, 18, 19,
                17, 18, 19, 18, 16, 14, 12, 11, 9, 7, 5, 3)
PAGES_PER_VISIT_WEIGHTS = (52, 22, 12, 7, 4, 3)


def positive_int(value):
    try:
        number = int(value)
    except ValueError as error:
        raise argparse.ArgumentTypeError(f"must be a number: {value}") from error
    if number < 1:
        raise argparse.ArgumentTypeError("must be at least 1")
    return number


def nonnegative_int(value):
    try:
        number = int(value)
    except ValueError as error:
        raise argparse.ArgumentTypeError(f"must be a number: {value}") from error
    if number < 0:
        raise argparse.ArgumentTypeError("must be at least 0")
    return number


def parse_args():
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n", 1)[0])
    parser.add_argument("-d", dest="days", type=positive_int, default=45,
                        help="days of history to generate (default: 45)")
    parser.add_argument("-n", dest="visits", type=positive_int, default=45,
                        help="rough visits per day (default: 45)")
    parser.add_argument("-s", dest="seed", type=nonnegative_int, default=1,
                        help="random seed (default: 1)")
    parser.add_argument("-u", dest="api_url",
                        default=os.getenv("GOATCOUNTER_SAMPLE_API_URL") or "http://localhost:8080/api",
                        help="API endpoint (default: %(default)s)")
    parser.add_argument("-t", dest="api_token", default=os.getenv("GOATCOUNTER_API_TOKEN", ""),
                        help="API bearer token (or set GOATCOUNTER_API_TOKEN)")
    parser.add_argument("-y", dest="yes", action="store_true",
                        help="do not ask for confirmation")
    args = parser.parse_args()
    if not args.api_token:
        parser.error("set GOATCOUNTER_API_TOKEN or pass -t")
    return args


def weighted_pick(rng, values, weight_index=-1):
    target = rng.random() * sum(value[weight_index] for value in values)
    total = 0
    for value in values:
        total += value[weight_index]
        if target <= total:
            return value
    return values[-1]


def generate_hits(days, visits, seed):
    rng = random.Random(seed)
    now = int(time.time())
    utc_now = datetime.fromtimestamp(now, timezone.utc)
    base = now - (utc_now.hour * 3600 + utc_now.minute * 60 + utc_now.second)
    today_dow = datetime.now().isoweekday()
    pages = [path for path in PATHS if not path[1]]
    events = [path for path in PATHS if path[1]]
    hits = []
    visit_count = 0

    for site in SITES:
        for day in range(days - 1, -1, -1):
            dow = (today_dow - 1 - day) % 7 + 1
            count = float(visits)
            count *= 0.55 if dow >= 6 else 1
            count *= 0.75 + 0.5 * ((days - 1 - day) / (days - 1) if days > 1 else 1)
            count *= 0.8 + rng.random() * 0.45
            if rng.random() < 0.055:
                count *= 2.2 + rng.random()

            for _ in range(int(count + 0.5)):
                session = f"{rng.getrandbits(128):032x}"
                device = weighted_pick(rng, DEVICES)
                width = rng.choice(device[4])
                location = weighted_pick(rng, LOCATIONS)[0]
                language = weighted_pick(rng, LANGUAGES)[0]
                second = weighted_pick(rng, tuple(enumerate(HOUR_WEIGHTS)))[0] * 3600
                second += int(rng.random() * 3600)

                campaign = None
                if rng.random() < 0.09:
                    campaign = weighted_pick(rng, CAMPAIGNS)
                    referrer, scheme = campaign[1], "c"
                else:
                    referrer, scheme, _ = weighted_pick(rng, REFERRERS)

                seen = set()
                page_count = weighted_pick(rng, tuple(enumerate(PAGES_PER_VISIT_WEIGHTS)))[0] + 1
                visit_paths = []
                for page_number in range(page_count):
                    path = weighted_pick(rng, PATHS) if page_number == 0 else rng.choice(pages)
                    if page_number:
                        second += rng.randint(20, 420)
                    visit_paths.append((path, second))
                if events and rng.random() < 0.12:
                    second += rng.randint(20, 420)
                    visit_paths.append((rng.choice(events), second))

                for path, created_second in visit_paths:
                    stamp = base - day * 86400 + created_second
                    if stamp > now:
                        continue
                    first_visit = path[0] not in seen
                    seen.add(path[0])
                    hits.append({
                        "site": site,
                        "path": path[0],
                        "event": path[1],
                        "ref": referrer,
                        "ref_scheme": scheme,
                        "campaign": campaign[0] if campaign else "",
                        "browser": device[0],
                        "browser_version": device[1],
                        "system": device[2],
                        "system_version": device[3],
                        "location": location,
                        "language": language,
                        "width": width,
                        "first_visit": first_visit,
                        "session": session,
                        "created_at_unix": stamp,
                    })
                visit_count += 1

    return hits, visit_count


def confirm(api_url, days):
    prompt = (f"Replace all pageviews and statistics in {api_url} with "
              f"{days} days of generated data? [y/N] ")
    try:
        with open("/dev/tty", "r+") as tty:
            tty.write(prompt)
            tty.flush()
            reply = tty.readline().strip()
    except OSError:
        reply = input(prompt).strip() if sys.stdin.isatty() else ""
    if reply not in ("y", "Y", "yes", "YES"):
        raise SystemExit("Aborted.")


def import_hits(api_url, api_token, hits):
    payload = json.dumps({"action": "import_raw", "arguments": {
        "replace": True, "hits": hits,
    }}, separators=(",", ":")).encode()
    request = urllib.request.Request(api_url, data=payload, headers={
        "Authorization": f"Bearer {api_token}",
        "Content-Type": "application/json",
    })
    try:
        with urllib.request.urlopen(request) as response:
            body = response.read().decode()
    except urllib.error.HTTPError as error:
        body = error.read().decode(errors="replace")
        if body:
            print(body, file=sys.stderr)
        raise SystemExit(f"API request failed: HTTP {error.code} {error.reason}") from error
    except urllib.error.URLError as error:
        raise SystemExit(f"API request failed: {error.reason}") from error
    except OSError as error:
        raise SystemExit(f"API request failed: {error}") from error
    if body:
        print(body)


def main():
    args = parse_args()
    if not args.yes:
        confirm(args.api_url, args.days)

    print(f"==> Generating {args.days} days of pageviews", flush=True)
    hits, visits = generate_hits(args.days, args.visits, args.seed)
    print(f"    {visits} visits, {len(hits)} pageviews over {args.days} days", file=sys.stderr)
    print("==> Importing through the bearer-protected API", flush=True)
    import_hits(args.api_url, args.api_token, hits)
    print(f"Done. Dashboard: {args.api_url.removesuffix('/api')}/")


if __name__ == "__main__":
    main()
