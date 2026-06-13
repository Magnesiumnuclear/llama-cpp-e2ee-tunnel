@echo off
chcp 65001 >nul
title Qwen3.6-35B-A3B 本地伺服器啟動器 (Context Shift 版)
cd /d D:\software\llama-b9196-bin-win-cuda-13.1-x64

:: ============================================================
:: 模型架構：Qwen3.6-35B-A3B 混合 MoE
::   - 40 層 = 10 區塊 x (3 層 Gated DeltaNet 線性注意力 + 1 層標準注意力)
::   - 只有 10 層(標準注意力)有「會長大」的 KV cache，其餘 30 層是固定大小循環狀態
::   - 256 個專家，每 token 只啟用 8 路由 + 1 共享，專家很小(intermediate 512)
::
:: 關於 CONTEXT SHIFT：
::   1. context shift 與 mmproj 互斥 → 載入 mmproj 會自動停用 shift。
::      純文字模式(1/2/4/5)已移除 --mmproj 以開啟 shift；
::      視覺模式(3)需要 mmproj，故不開 shift。
::   2. 本模型 3/4 的層是「無法做位置位移」的循環狀態。啟動後請看 log：
::      若出現  ctx_shift is not supported by this context  →
::      代表這顆模型在 llama.cpp 無法 context shift，請改用「大 -c + 前端管理歷史」。
::   3. shift 生效時：context 用完不會停，而是「保留開頭 n_keep，
::      丟棄保留區之後最舊的一半，位移後繼續生成」。不是逐字忘，是一次砍一大塊。
:: ============================================================

:menu
cls
echo ==================================================
echo      Qwen3.6-35B-A3B 啟動核心 (Context Shift 版)
echo ==================================================
echo.
echo  [1] Q4_K_M 平衡版 - 純文字 + context shift
echo      - 長程式除錯與長文本推理的主力配置
echo.
echo  [2] IQ2_M 速度版 - 純文字 + context shift
echo      - 最快噴字速度，適合長對話 / RP
echo.
echo  [3] IQ2_M 視覺版 - 載入 mmproj，無 context shift
echo      - 以速度版為基礎，啟用圖片 / 影片理解
echo.
echo  [4] Q8_K_P 高品質版 - 純文字 + context shift
echo      - 最高推理品質與程式碼審查 (吃近 38GB 記憶體)
echo.
echo  [5] Codex Agent 版 - 純文字 + context shift + --jinja
echo      - 供 IDE / agent 工具呼叫使用
echo.
echo  [6] 離開程序
echo ==================================================
set /p choice=請選擇要啟動的設定 1-6 : 
if "%choice%"=="1" goto run_q4
if "%choice%"=="2" goto run_iq2
if "%choice%"=="3" goto run_iq2_vision
if "%choice%"=="4" goto run_q8
if "%choice%"=="5" goto run_codex
if "%choice%"=="6" exit
echo 輸入錯誤，請重新選擇！
timeout /t 2 >nul
goto menu

:: ==================================================
:: 設定一：Q4_K_M 平衡版 (純文字)
:: 全部 40 層的注意力/共享層在 GPU，256 個專家全部交給 CPU/RAM。
:: 適合長程式除錯與推理；context 用完自動丟舊續寫(需 log 確認 shift 生效)。
:: ==================================================
:run_q4
cls
echo [Q4_K_M 平衡版] 啟動中...
echo 注意力層在 GPU、專家在 CPU/RAM；128K 視窗 + context shift。
echo --------------------------------------------------
llama-server.exe ^
 -m "models\Qwen3.6-35B-A3B-UD-Q4_K_M.gguf" ^
--mmproj "models\mmproj-BF16.gguf" ^
 -ngl 99 ^
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
 --host 127.0.0.1 ^
 --port 8080
pause
goto menu

:: ==================================================
:: 設定二：IQ2_M 速度版 (純文字)
:: ~11GB 量化模型，只把 9 層專家放 CPU、其餘專家留 GPU 衝速度。
:: 較短視窗 + context shift，長對話/RP 不中斷。
:: ==================================================
:run_iq2
cls
echo [IQ2_M 速度版] 啟動中...
echo 多數專家留在 GPU 衝速度；64K 視窗 + context shift。
echo --------------------------------------------------
llama-server.exe ^
 -m "models\Qwen3.6-35B-A3B-Uncensored-HauhauCS-Aggressive-IQ2_M.gguf" ^
 -ngl 99 ^
 --n-cpu-moe 9 ^
 --flash-attn on ^
 --parallel 1 ^
 -c 64000 ^
 -t 16 ^
 -b 256 ^
 -ub 256 ^
 --cache-type-k q4_0 ^
 --cache-type-v q4_0 ^
 --context-shift ^
 --mlock ^
 --host 127.0.0.1 ^
 --port 8080
pause
goto menu

:: ==================================================
:: 設定三：IQ2_M 視覺版 (以設定二為基礎 + 視覺)
:: 與速度版同權重，額外載入 mmproj 啟用圖片/影片理解。
:: 載入 mmproj 後 llama-server 會自動停用 context shift，故本模式不加 --context-shift。
:: 注意：IQ2 + mmproj + 影像 token 會吃掉額外 VRAM；若處理圖片時 OOM，
::       調高 --n-cpu-moe(把更多專家移到 CPU 騰出顯存) 或調低 -c。
::       (IQ2 為 2-bit 激進量化，視覺推理品質會比 Q4/Q8 弱，需要更準時可改用 Q4 權重)
:: ==================================================
:run_iq2_vision
cls
echo [IQ2_M 視覺版] 啟動中...
echo 載入 mmproj 啟用視覺；context shift 不適用(自動停用)。
echo --------------------------------------------------
llama-server.exe ^
 -m "models\Qwen3.6-35B-A3B-Uncensored-HauhauCS-Aggressive-IQ2_M.gguf" ^
 --mmproj "models\mmproj-BF16.gguf" ^
 -ngl 99 ^
 --n-cpu-moe 9 ^
 --flash-attn on ^
 --parallel 1 ^
 -c 64000 ^
 -t 16 ^
 -b 256 ^
 -ub 256 ^
 --cache-type-k q4_0 ^
 --cache-type-v q4_0 ^
 --mlock ^
 --host 127.0.0.1 ^
 --port 8080
pause
goto menu

:: ==================================================
:: 設定四：Q8_K_P 高品質版 (純文字)
:: 近 38GB 進系統記憶體，專家全在 CPU/RAM；最高品質推理/程式碼審查。
:: 啟動前確保背景無大型耗能程式，避免 swap。
:: ==================================================
:run_q8
cls
echo [Q8_K_P 高品質版] 啟動中...
echo 警告：將吃近 38GB 系統記憶體，請關閉背景大型程式。
echo --------------------------------------------------
llama-server.exe ^
 -m "models\Qwen3.6-35B-A3B-Uncensored-HauhauCS-Aggressive-Q8_K_P.gguf" ^
 -ngl 99 ^
 --n-cpu-moe 99 ^
 --flash-attn on ^
 --parallel 1 ^
 -c 131072 ^
 -t 16 ^
 -b 2048 ^
 -ub 2048 ^
 --cache-type-k q4_0 ^
 --cache-type-v q4_0 ^
 --context-shift ^
 --mlock ^
 --host 127.0.0.1 ^
 --port 8080
pause
goto menu

:: ==================================================
:: 設定五：Codex Agent 版 (純文字)
:: 與設定一同權重，額外開 --jinja 載入模型對話模板
:: (工具呼叫 / think 標籤解析)，供 IDE / agent 使用。
:: ==================================================
:run_codex
cls
echo [Codex Agent 版] 啟動中...
echo 同 Q4 權重 + --jinja(工具模板)；128K 視窗 + context shift。
echo --------------------------------------------------
llama-server.exe ^
 -m "models\Qwen3.6-35B-A3B-UD-Q4_K_M.gguf" ^
 -ngl 99 ^
 --n-cpu-moe 99 ^
 --flash-attn on ^
 --parallel 1 ^
 -c 131072 ^
 -t 16 ^
 -b 4096 ^
 -ub 4096 ^
 --jinja ^
 --cache-type-k q4_0 ^
 --cache-type-v q4_0 ^
 --context-shift ^
 --mlock ^
 --host 127.0.0.1 ^
 --port 8080
pause
goto menu