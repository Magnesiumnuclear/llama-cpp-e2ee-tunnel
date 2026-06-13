@echo off
:: 1. 設定局部環境變數：強制將這支程式的 API 請求劫持到本地端 (CC Switch)
:: 注意：請將 11434 改成你 CC Switch 實際顯示的網關 Port
set CLAUDE_BASE_URL=http://127.0.0.1:11434/v1
set ANTHROPIC_API_KEY=123456

:: 2. 設定局部環境變數：開啟 Claude 隱藏的開發者模式
set CLAUDE_ENABLE_DEV_TOOLS=true
set ENABLE_DEVELOPER_MODE=true

:: 3. 利用 Electron 原生參數，強制將這個本地版的設定檔與快取，存到獨立的新資料夾
start "" "C:\Users\samho\AppData\Local\AnthropicClaude\app-1.11847.5\claude.exe" --user-data-dir="C:\Claude_Local_Profile"