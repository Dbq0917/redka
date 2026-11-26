# Server/connection management

Redka supports only a couple of server and connection management commands:

```
Command    Go API                Description
-------    ------                -----------
ECHO       -                     Returns the given string.
INFO       DB.Stats / DB.Key().Len  Returns connection, command, key, and expiration statistics.
LOLWUT     -                     Provides an answer to a yes/no question.
PING       -                     Returns the server's liveliness response.
SELECT     -                     Changes the selected database (no-op).
```

The rest of the server and connection management commands are not supported.
