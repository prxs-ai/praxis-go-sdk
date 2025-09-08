#!/usr/bin/env python3
"""
Simplified Twitter Scraper using Apify API with requests
"""
import json
import sys
import os
import argparse
import datetime
import requests
import time

def main():
    parser = argparse.ArgumentParser(description='Twitter scraper for Dagger using Apify API')
    parser.add_argument('--username', type=str, help='Twitter username/handle to scrape (without @)')
    parser.add_argument('--tweets_count', type=int, default=50, help='Number of tweets to scrape (default: 50)')
    
    args = parser.parse_args()
    
    if not args.username:
        print(json.dumps({
            "status": "error",
            "message": "No username provided. Use --username parameter."
        }))
        return
    
    # Get Apify API token from environment
    apify_token = os.environ.get('APIFY_API_TOKEN')
    if not apify_token:
        print(json.dumps({
            "status": "error",
            "message": "APIFY_API_TOKEN environment variable not set"
        }))
        return
    
    try:
        # Clean username (remove @ if present)
        username = args.username.lstrip('@')
        
        # Prepare the Actor input
        run_input = {
            "handles": [username],
            "userQueries": [],
            "tweetsDesired": args.tweets_count,
            "profilesDesired": 1,
            "proxyConfig": {
                "useApifyProxy": True,
                "apifyProxyGroups": ["RESIDENTIAL"],
            },
        }
        
        print(json.dumps({
            "status": "processing",
            "message": f"🐦 Scraping {args.tweets_count} tweets from @{username}..."
        }))
        
        # Start the Actor run
        headers = {
            "Content-Type": "application/json"
        }
        
        # Run the Actor
        actor_url = f"https://api.apify.com/v2/acts/web.harvester~twitter-scraper/runs?token={apify_token}"
        response = requests.post(actor_url, json=run_input, headers=headers)
        
        if response.status_code != 201:
            raise Exception(f"Failed to start actor: {response.text}")
        
        run_data = response.json()
        run_id = run_data['data']['id']
        
        # Wait for the run to finish
        max_wait = 60  # seconds
        waited = 0
        while waited < max_wait:
            status_url = f"https://api.apify.com/v2/acts/web.harvester~twitter-scraper/runs/{run_id}?token={apify_token}"
            status_response = requests.get(status_url)
            status_data = status_response.json()
            
            if status_data['data']['status'] in ['SUCCEEDED', 'FAILED', 'ABORTED']:
                break
            
            time.sleep(5)
            waited += 5
        
        if status_data['data']['status'] != 'SUCCEEDED':
            raise Exception(f"Actor run failed with status: {status_data['data']['status']}")
        
        # Get the dataset
        dataset_id = status_data['data']['defaultDatasetId']
        dataset_url = f"https://api.apify.com/v2/datasets/{dataset_id}/items?token={apify_token}"
        dataset_response = requests.get(dataset_url)
        items = dataset_response.json()
        
        if not items:
            print(json.dumps({
                "status": "error",
                "message": f"No data found for username @{username}. Check if the username exists and is public."
            }))
            return
        
        # Process the results
        tweets = []
        profile_data = None
        
        for item in items:
            if item.get('type') == 'tweet':
                tweets.append({
                    'id': item.get('id'),
                    'text': item.get('text', ''),
                    'created_at': item.get('createdAt'),
                    'retweet_count': item.get('retweetCount', 0),
                    'like_count': item.get('likeCount', 0),
                    'reply_count': item.get('replyCount', 0),
                    'url': item.get('url', '')
                })
            elif item.get('type') == 'profile' or 'followersCount' in item:
                profile_data = {
                    'username': item.get('username', username),
                    'display_name': item.get('name', ''),
                    'description': item.get('description', ''),
                    'followers_count': item.get('followersCount', 0),
                    'following_count': item.get('followingCount', 0),
                    'tweets_count': item.get('tweetsCount', 0),
                    'verified': item.get('verified', False)
                }
        
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
                "apify_dataset_url": f"https://console.apify.com/storage/datasets/{dataset_id}"
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