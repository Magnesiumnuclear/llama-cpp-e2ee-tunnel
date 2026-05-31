@echo off
chcp 65001 >nul
title Codex + Qwen3.6 本地大腦 一鍵啟動

:: ==================================================
:: 設定路徑（如有不同請自行修改）
:: ==================================================
set "LLAMA_DIR=D:\software\llama-b9196-bin-win-cuda-13.1-x64"
set "CODEX_DIR=D:\codex_Sandbox"
set "SERVER_URL=http://127.0.0.1:8080/v1/models"

cls
echo ==================================================
echo    Codex CLI + llama.cpp (Codex 代碼補全版) 一鍵啟動
echo ==================================================
echo.

:: ==================================================
:: 步驟 1：在新視窗啟動 llama-server
:: ==================================================
echo [1/3] 正在新視窗啟動 llama-server...
cd /d "%LLAMA_DIR%"
start "llama-server (Codex 補全版)" cmd /c ^
 llama-server.exe ^
 -m "models\Qwen3.6-35B-A3B-UD-Q4_K_M.gguf" ^
 --mmproj "models\mmproj-BF16.gguf" ^
 -ngl 64 ^
 --n-cpu-moe 99 ^
 --flash-attn on ^
 --parallel 1 ^
 -c 131072 ^
 -t 16 ^
 -b 4096 ^
 -ub 4096 ^
 --cache-type-k q4_0 ^
 --cache-type-v q4_0 ^
 --mlock ^
 --jinja ^
 --host 127.0.0.1 ^
 --port 8080

:: ==================================================
:: 步驟 2：等待 server 就緒（輪詢 /v1/models）
:: ==================================================
echo.
echo [2/3] 等待模型載入中（這可能需要 1-3 分鐘）...
echo       偵測位置：%SERVER_URL%
echo.

:wait_loop
timeout /t 5 >nul
curl -s -o nul "%SERVER_URL%"
if errorlevel 1 (
    echo       還在載入... 持續等待
    goto wait_loop
)

echo.
echo ✅ llama-server 已就緒！
echo.

:: ==================================================
:: 步驟 3：啟動 Codex CLI
:: ==================================================
echo [3/3] 正在啟動 Codex CLI (profile: local-quality)...
echo --------------------------------------------------
cd /d "%CODEX_DIR%"
codex --profile local-quality

:: Codex 結束後回到這裡
echo.
echo ==================================================
echo Codex 已結束。llama-server 仍在另一個視窗運行中。
echo 如需關閉 server，請直接關掉那個視窗。
echo ==================================================
pause