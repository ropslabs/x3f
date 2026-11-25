# Test fixture sources

## Telegram Bot API (`telegram/`)

All fixtures are **derived**, not recorded: they follow the response format of
the official Bot API documentation and the source code of the official Bot API
server, [tdlib/telegram-bot-api](https://github.com/tdlib/telegram-bot-api) at
commit `5df614c91dab8662cfbb24a501ec89949fdcfebb`. Checked on 2026-11-25.

Response format, from <https://core.telegram.org/bots/api#making-requests>:
"The response contains a JSON object, which always has a Boolean field 'ok' and
may have an optional String field 'description' […]. If 'ok' equals True, the
request was successful and the result of the query can be found in the 'result'
field. In case of an unsuccessful request, 'ok' equals False and the error is
explained in the 'description'. An Integer 'error_code' field is also returned
[…]. Some errors may also have an optional field 'parameters' of the type
ResponseParameters". `ResponseParameters.retry_after`: "In case of exceeding
flood control, the number of seconds left to wait before the request can be
repeated".

The server writes errors with `JsonQueryError::store` (fields `ok`,
`error_code`, `description`, `parameters`, in this order,
[Query.cpp#L27](https://github.com/tdlib/telegram-bot-api/blob/5df614c91dab8662cfbb24a501ec89949fdcfebb/telegram-bot-api/Query.cpp#L27))
and uses the error code as the HTTP status
([Query.h#L263](https://github.com/tdlib/telegram-bot-api/blob/5df614c91dab8662cfbb24a501ec89949fdcfebb/telegram-bot-api/Query.h#L263)).

| File | HTTP | Source of the description |
|---|---|---|
| `ok.json` | 200 | `JsonQueryOk` ([Query.h#L196](https://github.com/tdlib/telegram-bot-api/blob/5df614c91dab8662cfbb24a501ec89949fdcfebb/telegram-bot-api/Query.h#L196)); `result` is a Message reduced to its required fields `message_id`, `date`, `chat` (`id`, `type`) plus `text`, per the documentation. IDs, date and text are filled in. |
| `unauthorized.json` | 401 | `"Unauthorized"`, [Client.cpp#L18362](https://github.com/tdlib/telegram-bot-api/blob/5df614c91dab8662cfbb24a501ec89949fdcfebb/telegram-bot-api/Client.cpp#L18362) |
| `chat-not-found.json` | 400 | `"Bad Request: chat not found"`, [Client.cpp#L7743](https://github.com/tdlib/telegram-bot-api/blob/5df614c91dab8662cfbb24a501ec89949fdcfebb/telegram-bot-api/Client.cpp#L7743) |
| `too-many-requests.json` | 429 | `set_retry_after_error`: `"Too Many Requests: retry after " << retry_after` and `parameters.retry_after`, [Query.cpp#L120](https://github.com/tdlib/telegram-bot-api/blob/5df614c91dab8662cfbb24a501ec89949fdcfebb/telegram-bot-api/Query.cpp#L120); the value 5 is the one the server uses in [Query.h#L242](https://github.com/tdlib/telegram-bot-api/blob/5df614c91dab8662cfbb24a501ec89949fdcfebb/telegram-bot-api/Query.h#L242) |
| `internal-server-error.json` | 500 | `"Internal Server Error: restart"`, [Client.cpp#L18367](https://github.com/tdlib/telegram-bot-api/blob/5df614c91dab8662cfbb24a501ec89949fdcfebb/telegram-bot-api/Client.cpp#L18367) |

The request format (`chat_id` as Integer or String, `text` with "1-4096
characters after entities parsing", `parse_mode`, `link_preview_options` with
`is_disabled`) and the HTML escaping rules ("All `<`, `>` and `&` symbols that
are not a part of a tag or an HTML entity must be replaced with the
corresponding HTML entities"; "All numerical HTML entities are supported") are
from <https://core.telegram.org/bots/api#sendmessage> and
<https://core.telegram.org/bots/api#html-style>.
