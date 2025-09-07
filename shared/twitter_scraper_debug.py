#!/usr/bin/env python3
"""
Debug Twitter Scraper using Apify REST API with hardcoded token
Purpose: isolate Dagger/env issues and verify Apify actor returns data.

Usage examples:
  python twitter_scraper_debug.py --username elonmusk --tweets_count 30

Notes:
- This script prefers APIFY_API_TOKEN from env, but falls back to the hardcoded token below.
- You can optionally override actor via APIFY_ACTOR_ID (default: web.harvester~twitter-scraper).
- You can optionally set APIFY_PROXY_GROUPS (e.g., RESIDENTIAL) if your plan supports it.
"""
import json
import os
import argparse
import datetime
import time
import sys
from typing import List, Dict, Any

import urllib.request
import urllib.error
import urllib.parse


HARDCODED_APIFY_TOKEN = ""


def run_actor(token: str, actor_id: str, username: str, tweets_count: int, proxy_groups: List[str]) -> Dict[str, Any]:
    username = username.lstrip('@')

    # Prepare input depending on actor
    if actor_id.startswith("apidojo~tweet-scraper"):
        # Payload aligned with apidojo actor's search mode
        run_input = {
            "searchMode": "live",
            "searchTerms": [f"from:{username}"],
            "maxItems": int(tweets_count),
            # Optional filters:
            # "tweetLanguage": "en",
            # "includeRetweets": True,
            # "includeReplies": False,
        }
    else:
        proxy_config: Dict[str, Any] = {"useApifyProxy": True}
        if proxy_groups:
            proxy_config["apifyProxyGroups"] = proxy_groups

        run_input = {
            "handles": [username],
            "userQueries": [],
            "tweetsDesired": tweets_count,
            "profilesDesired": 1,
            "proxyConfig": proxy_config,
        }

    headers = {"Content-Type": "application/json"}
    start_url = f"https://api.apify.com/v2/acts/{actor_id}/runs?token={token}"
    req = urllib.request.Request(
        start_url,
        data=json.dumps(run_input).encode('utf-8'),
        headers=headers,
        method='POST',
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            if resp.status != 201:
                body = resp.read().decode('utf-8', errors='ignore')
                raise RuntimeError(f"Failed to start actor: {resp.status} {body}")
            run_json = json.loads(resp.read().decode('utf-8'))
    except urllib.error.HTTPError as e:
        body = e.read().decode('utf-8', errors='ignore')
        raise RuntimeError(f"Failed to start actor: {e.code} {body}") from e
    except urllib.error.URLError as e:
        raise RuntimeError(f"Failed to start actor (network): {e}") from e

    run_data = run_json["data"]
    run_id = run_data["id"]
    print(json.dumps({"status": "processing", "message": f"Run started: {run_id}"}))

    status_url = f"https://api.apify.com/v2/acts/{actor_id}/runs/{run_id}?token={token}"
    max_wait = 120
    waited = 0
    while waited < max_wait:
        try:
            with urllib.request.urlopen(status_url, timeout=30) as resp:
                st_json = json.loads(resp.read().decode('utf-8'))
        except urllib.error.HTTPError as e:
            body = e.read().decode('utf-8', errors='ignore')
            raise RuntimeError(f"Status check failed: {e.code} {body}") from e
        except urllib.error.URLError as e:
            raise RuntimeError(f"Status check failed (network): {e}") from e

        st = st_json["data"]
        status = st.get("status")
        if status in ("SUCCEEDED", "FAILED", "ABORTED", "TIMED-OUT"):
            break
        time.sleep(5)
        waited += 5
        print(json.dumps({"status": "processing", "message": f"Waiting... status={status}, waited={waited}s"}))

    if status != "SUCCEEDED":
        raise RuntimeError(f"Actor run finished with status: {status}")

    dataset_id = st.get("defaultDatasetId")
    if not dataset_id:
        raise RuntimeError("No dataset id returned")

    items_url = f"https://api.apify.com/v2/datasets/{dataset_id}/items?token={token}"
    try:
        with urllib.request.urlopen(items_url, timeout=60) as resp:
            items_json = json.loads(resp.read().decode('utf-8'))
    except urllib.error.HTTPError as e:
        body = e.read().decode('utf-8', errors='ignore')
        raise RuntimeError(f"Failed to fetch dataset items: {e.code} {body}") from e
    except urllib.error.URLError as e:
        raise RuntimeError(f"Failed to fetch dataset items (network): {e}") from e

    return {
        "items": items_json,
        "dataset_id": dataset_id,
    }


def main():
    parser = argparse.ArgumentParser(description="Debug Twitter scraper (Apify REST)")
    parser.add_argument("--username", required=True, help="Twitter handle without @")
    parser.add_argument("--tweets_count", type=int, default=50)
    args = parser.parse_args()

    token = os.environ.get("APIFY_API_TOKEN") or HARDCODED_APIFY_TOKEN
    if not token or not token.startswith("apify_api_"):
        print(json.dumps({"status": "error", "message": "Missing/invalid APIFY token"}))
        sys.exit(1)

    actor_id = os.environ.get("APIFY_ACTOR_ID", "web.harvester~twitter-scraper")
    proxy_groups_env = os.environ.get("APIFY_PROXY_GROUPS", "RESIDENTIAL")
    proxy_groups = [s.strip() for s in proxy_groups_env.split(',') if s.strip()] if proxy_groups_env else []

    try:
        out = run_actor(token, actor_id, args.username, args.tweets_count, proxy_groups)
        items = out["items"]
        if not items:
            print(json.dumps({"status": "error", "message": f"No data found for @{args.username}"}))
            sys.exit(2)

        tweets = []
        profile = None
        for it in items:
            if it.get("type") == "tweet":
                tweets.append({
                    "id": it.get("id"),
                    "text": it.get("text", ""),
                    "created_at": it.get("createdAt"),
                    "retweet_count": it.get("retweetCount", 0),
                    "like_count": it.get("likeCount", 0),
                    "reply_count": it.get("replyCount", 0),
                    "url": it.get("url", ""),
                })
            elif it.get("type") == "profile" or "followersCount" in it:
                profile = {
                    "username": it.get("username", args.username),
                    "display_name": it.get("name", ""),
                    "description": it.get("description", ""),
                    "followers_count": it.get("followersCount", 0),
                    "following_count": it.get("followingCount", 0),
                    "tweets_count": it.get("tweetsCount", 0),
                    "verified": it.get("verified", False),
                }

        ts = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
        reports_dir = os.path.join(os.path.dirname(__file__), "reports")
        os.makedirs(reports_dir, exist_ok=True)
        fname = f"twitter_{args.username}_{ts}.json"
        fpath = os.path.join(reports_dir, fname)
        with open(fpath, "w", encoding="utf-8") as f:
            json.dump({
                "metadata": {
                    "scraped_at": datetime.datetime.now().isoformat(),
                    "username": args.username,
                    "tweets_requested": args.tweets_count,
                    "tweets_found": len(tweets),
                },
                "profile": profile,
                "tweets": tweets,
                "dataset_url": f"https://console.apify.com/storage/datasets/{out['dataset_id']}",
            }, f, ensure_ascii=False, indent=2)

        print(json.dumps({
            "status": "success",
            "message": f"Fetched {len(tweets)} tweets for @{args.username}",
            "saved_to": fpath,
            "sample": tweets[:5],
        }, ensure_ascii=False))

    except Exception as e:
        print(json.dumps({"status": "error", "message": str(e)}))
        sys.exit(3)


if __name__ == "__main__":
    main()
