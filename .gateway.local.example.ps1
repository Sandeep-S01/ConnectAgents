# Copy this file to .gateway.local.ps1 and fill in your private values.
# Do not commit .gateway.local.ps1.

$env:PAIA_ADDR = "127.0.0.1:8080"
$env:PAIA_DB_PATH = "data/gateway.db"
$env:PAIA_LOG_PATH = "logs/gateway.log"
$env:PAIA_LOG_MAX_BYTES = "10485760"
$env:PAIA_TASK_TIMEOUT_SECONDS = "7200"

# Use at least 32 characters when PAIA_ENV is production.
$env:PAIA_AUTH_TOKEN = "change-this-token-to-at-least-32-chars"

# Fill these after creating the Telegram bot.
$env:PAIA_TELEGRAM_BOT_TOKEN = ""
$env:PAIA_TELEGRAM_USER_ID = ""

# Optional OpenRouter settings.
$env:OPENROUTER_API_KEY = ""
$env:OPENROUTER_MODEL = "openrouter/free"
$env:OPENROUTER_BASE_URL = "https://openrouter.ai/api/v1"
