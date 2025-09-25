#!/usr/bin/env python3
import json
import sys
import os
import argparse

def main():
    parser = argparse.ArgumentParser(description='Python analyzer for Dagger')
    parser.add_argument('--input_file', type=str, help='Input file to analyze')
    
    args = parser.parse_args()
    
    if not args.input_file:
        print(json.dumps({
            "status": "error",
            "message": "No input file provided"
        }))
        return
    
    input_file = args.input_file
    # Если путь уже содержит /shared/, используем его как есть
    if input_file.startswith('/shared/'):
        input_path = input_file
        input_file = input_file.replace('/shared/', '')
    else:
        input_path = f"/shared/{input_file}"
    
    try:
        if os.path.exists(input_path):
            with open(input_path, 'r') as f:
                content = f.read()
            
            analysis_result = {
                "status": "success",
                "message": f"Python analyzer executed successfully via Dagger",
                "input_file": input_file,
                "content_length": len(content),
                "content_preview": content[:100] + "..." if len(content) > 100 else content,
                "analysis": {
                    "word_count": len(content.split()),
                    "line_count": len(content.splitlines()),
                    "has_numbers": any(c.isdigit() for c in content)
                }
            }
        else:
            analysis_result = {
                "status": "error",
                "message": f"Input file {input_file} not found in /shared/",
                "available_files": os.listdir("/shared") if os.path.exists("/shared") else []
            }
    
    except Exception as e:
        analysis_result = {
            "status": "error", 
            "message": f"Error processing file: {str(e)}"
        }
    
    print(json.dumps(analysis_result, indent=2))

if __name__ == "__main__":
    main()
