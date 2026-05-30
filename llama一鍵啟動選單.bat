@echo off
chcp 65001 >nul
title Qwen3.6-35B 本地雙模態大腦伺服器
cd /d D:\software\llama-b9196-bin-win-cuda-13.1-x64
:menu
cls
echo ==================================================
echo      Qwen3.6-35B 雙晶片規格獨立調校核心
echo ==================================================
echo.
echo  [1] 啟動 Q4_K_M 完美平衡版 - 混合運算
echo      - 適合重視智商、複雜程式除錯與長文本任務
echo.
echo  [2] 啟動 IQ2_M 越獄輕量版 - 純 GPU 狂暴模式
echo      - 整個模型全塞 12G 顯存，解鎖極致噴字速度
echo.
echo  [3] 啟動 Q8_K_P 究極滿血版 - 最高智商/無碼 (NEW)
echo      - 38GB 記憶體怪獸，極致推理與無損程式碼審查
echo.
echo  [4] 啟動 Codex 代碼補全版 - Agent 輔助編程
echo      - 輕量級配置，為 IDE 代碼補全優化
echo.
echo  [5] 離開程序
echo ==================================================
set /p choice=請選擇你想喚醒的大腦 1-5 : 
if "%choice%"=="1" goto run_q4
if "%choice%"=="2" goto run_iq2
if "%choice%"=="3" goto run_q8
if "%choice%"=="4" goto run_codex
if "%choice%"=="5" exit
echo ❌ 輸入錯誤，請重新選擇！
timeout /t 2 >nul
goto menu
:: ==================================================
:: 🧠 設定一：Q4_K_M 完美平衡版
:: ==================================================
:run_q4
cls
echo 🚀 正在啟動 [Q4_K_M A3B MoE 滿血狂飆模式]...
echo 💡 優化：核心網路鎖定 4080，專家層交付 i9 協同，恢復 40 t/s 榮光！
echo --------------------------------------------------
llama-server.exe ^
 -m "models\Qwen3.6-35B-A3B-UD-Q4_K_M.gguf" ^
 --mmproj "models\mmproj-BF16.gguf" ^
 -ngl 99 ^
 --n-cpu-moe 999 ^
 --flash-attn on ^
 -c 262144 ^
 -t 16 ^
 -b 2048 ^
 -ub 2048 ^
 --cache-type-k q4_0 ^
 --cache-type-v q4_0 ^
 --mlock ^
 --host 127.0.0.1 ^
 --port 8080
pause
goto menu
:: ==================================================
:: ⚡ 設定二：IQ2_M 越獄輕量版
:: ==================================================
:run_iq2
cls
echo 🚀 正在啟動 [IQ2_M 純 GPU 狂暴模式]...
echo 💡 提示：11GB 模型將完全鎖進 12G VRAM，體驗地表最快本地速度！
echo --------------------------------------------------
llama-server.exe ^
 -m "models\Qwen3.6-35B-A3B-Uncensored-HauhauCS-Aggressive-IQ2_M.gguf" ^
 --mmproj "models\mmproj-BF16.gguf" ^
 -ngl 999 ^
  --n-cpu-moe 9 ^
 --flash-attn on ^
  --parallel 1 ^
 -c 65536 ^
 -t 16 ^
 -b 512 ^
 -ub 521 ^
 --cache-type-k q4_0 ^
 --cache-type-v q4_0 ^
 --mlock ^
 --host 127.0.0.1 ^
 --port 8080
pause
goto menu
:: ==================================================
:: 🔥 設定三：Q8_K_P 究極滿血版
:: ==================================================
:run_q8
cls
echo 🚀 正在啟動 [Q8_K_P 究極滿血無碼模式]...
echo 💡 警告：即將吞噬近 40GB 系統記憶體，請確保背景無大型耗能軟體！
echo --------------------------------------------------
llama-server.exe ^
 -m "models\Qwen3.6-35B-A3B-Uncensored-HauhauCS-Aggressive-Q8_K_P.gguf" ^
 --mmproj "models\mmproj-BF16.gguf" ^
 -ngl 99 ^
 --n-cpu-moe 999 ^
 --flash-attn on ^
 -c 32768 ^
 -t 16 ^
 -b 512 ^
 -ub 128 ^
 --cache-type-k q4_0 ^
 --cache-type-v q4_0 ^
 --mlock ^
 --host 127.0.0.1 ^
 --port 8080
pause
goto menu
:: ==================================================
:: 💻 設定四：Codex 代碼補全版
:: ==================================================
:run_codex
cls
echo 🚀 正在啟動 [Codex 代碼補全優化版]...
echo 💡 優化：輕量級配置，低上下文 + 快速推理，為 IDE 補全設計
echo --------------------------------------------------
llama-server.exe ^
 -m "models\Qwen3.6-35B-A3B-UD-Q4_K_M.gguf" ^
 --mmproj "models\mmproj-BF16.gguf" ^
 -ngl 99 ^
  --n-cpu-moe 999 ^
 --flash-attn on ^
 --jinja ^
 -c 16384 ^
 -n 2048 ^
 -b 512 ^
 -ub 128 ^
 --cache-type-k q4_0 ^
 --cache-type-v q4_0 ^
 --mlock ^
 --host 127.0.0.1 ^
 --port 8080
pause
goto menu