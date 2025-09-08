#!/usr/bin/env python3
"""
Twitter Scraper using Apify API
Scrapes tweets from a specified Twitter username/channel
"""
import json
import sys
import os
import argparse
import datetime
from apify_client import ApifyClient

def main():
    parser = argparse.ArgumentParser(description='Twitter scraper for Dagger using Apify API')
    parser.add_argument('--username', type=str, help='Twitter username/handle to scrape (without @)')
    parser.add_argument('--tweets_count', type=int, default=50, help='Number of tweets to scrape (default: 50)')
    
    args = parser.parse_args()
    
    # Support username from environment (for container engines passing env vars)
    username = args.username or os.environ.get('username') or os.environ.get('USERNAME') or os.environ.get('TWITTER_USERNAME')
    if not username:
        print(json.dumps({
            "status": "error",
            "message": "No username provided. Use --username parameter or set env var 'username'/'USERNAME'/'TWITTER_USERNAME'."
        }))
        return
    
    # Get Apify API token from environment (do not hardcode tokens)
    apify_token = ""
    if not apify_token:
        print(json.dumps({
            "status": "error",
            "message": "APIFY_API_TOKEN environment variable not set"
        }))
        return
    
    try:
        # Initialize the ApifyClient
        client = ApifyClient(apify_token)
        
        # Clean username (remove @ if present)
        username = username.lstrip('@')
        
        # Select actor and prepare input
        actor_id = os.environ.get('APIFY_ACTOR_ID', '').strip()
        tweets_desired = int(os.environ.get('tweets_count', args.tweets_count))

        if actor_id.startswith('apidojo~tweet-scraper') or actor_id.startswith('apidojo/tweet-scraper') or actor_id == '':
            # Default to apidojo actor if not specified
            if not actor_id:
                actor_id = 'apidojo~tweet-scraper'

            query = os.environ.get('QUERY') or f"from:{username}"
            run_input = {
                "searchMode": "live",
                "searchTerms": [query],
                "maxItems": tweets_desired,
            }
            if os.environ.get('TWEET_LANGUAGE'):
                run_input["tweetLanguage"] = os.environ.get('TWEET_LANGUAGE')
            if os.environ.get('INCLUDE_RETWEETS'):
                run_input["includeRetweets"] = os.environ.get('INCLUDE_RETWEETS').lower() in ('1', 'true', 'yes')
            if os.environ.get('INCLUDE_REPLIES'):
                run_input["includeReplies"] = os.environ.get('INCLUDE_REPLIES').lower() in ('1', 'true', 'yes')
        else:
            # web.harvester input (requires paid/rented actor)
            proxy_config = {"useApifyProxy": True}
            proxy_groups = os.environ.get('APIFY_PROXY_GROUPS')
            if proxy_groups:
                groups = [g.strip() for g in proxy_groups.split(',') if g.strip()]
                if groups:
                    proxy_config["apifyProxyGroups"] = groups

            run_input = {
                "handles": [username],
                "userQueries": [],
                "tweetsDesired": tweets_desired,
                "profilesDesired": 1,
                "proxyConfig": proxy_config,
            }
        
        print(json.dumps({
            "status": "processing",
            "message": f"🐦 Scraping {args.tweets_count} tweets from @{username}..."
        }))
        
        # Run the Actor and wait for it to finish
        run = client.actor(actor_id).call(run_input=run_input, timeout_secs=300)
        
        # Fetch results from the dataset
        dataset_id = run.get("defaultDatasetId")
        items = list(client.dataset(dataset_id).iterate_items()) if dataset_id else []
        
        if not items:
            print(json.dumps({
                "status": "error",
                "message": f"No data found for username @{username}. Check if the username exists and is public."
            }))
            return
        
        # Separate tweets and profile data
        tweets = []
        profile_data = None
        
        for item in items:
            # Profile-like data
            if item.get('type') == 'profile' or 'followersCount' in item:
                profile_data = {
                    'username': item.get('username', username),
                    'display_name': item.get('name', ''),
                    'description': item.get('description', ''),
                    'followers_count': item.get('followersCount', 0),
                    'following_count': item.get('followingCount', 0),
                    'tweets_count': item.get('tweetsCount', 0),
                    'verified': item.get('verified', False)
                }
                continue

            # Tweet-like data (normalize keys across actors)
            text_val = item.get('text') or item.get('full_text') or item.get('content', '')
            created_val = item.get('createdAt') or item.get('created_at') or item.get('date')
            retweets_val = item.get('retweetCount', item.get('retweet_count', 0))
            likes_val = item.get('likeCount', item.get('favorite_count', item.get('like_count', 0)))
            replies_val = item.get('replyCount', item.get('reply_count', 0))
            url_val = item.get('url') or item.get('tweetUrl') or ''
            tid = item.get('id') or item.get('id_str')

            if text_val or url_val:
                tweets.append({
                    'id': tid,
                    'text': text_val,
                    'created_at': created_val,
                    'retweet_count': retweets_val,
                    'like_count': likes_val,
                    'reply_count': replies_val,
                    'url': url_val
                })
        
        # Create timestamp for file naming
        timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
        
        # Prepare full data for JSON export
        full_data = {
            'metadata': {
                'scraped_at': datetime.datetime.now().isoformat(),
                'username': username,
                'tweets_requested': args.tweets_count,
                'tweets_found': len(tweets)
            },
            'profile': profile_data,
            'tweets': tweets
        }
        
        # Save full data to JSON file
        reports_dir = "/shared/reports"
        os.makedirs(reports_dir, exist_ok=True)

        json_filename = f"twitter_{username}_{timestamp}.json"
        json_filepath = os.path.join(reports_dir, json_filename)

        with open(json_filepath, 'w', encoding='utf-8') as f:
            json.dump(full_data, f, ensure_ascii=False, indent=2)
        
        # Prepare summary for UI chat
        summary_tweets = tweets[:5]  # Show first 5 tweets in summary
        
        result = {
            "status": "success",
            "message": f"✅ Successfully scraped {len(tweets)} tweets from @{username}",
            "data": {
                "username": username,
                "profile": profile_data,
                "tweets_count": len(tweets),
                "latest_tweets": summary_tweets,
                "saved_to": json_filename,
                "saved_to_path": json_filepath,
                "apify_dataset_url": f"https://console.apify.com/storage/datasets/{dataset_id}",
                # Provide a direct download URL via agent HTTP server (static route /reports)
                "download_url": f"{os.environ.get('AGENT_BASE_URL', 'http://localhost:8000')}/reports/{json_filename}"
            }
        }
        
    except Exception as e:
        result = {
            "status": "error",
            "message": f"Error scraping Twitter data: {str(e)}"
        }
    
    print(json.dumps(result, ensure_ascii=False, indent=2))

if __name__ == "__main__":
    main()
