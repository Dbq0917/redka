# Transactions

Redka supports the following transaction commands:

```
Command    Go API                 Description
-------    ------                 -----------
DISCARD    DB.View / DB.Update    Discards a transaction.
EXEC       DB.View / DB.Update    Executes all commands in a transaction.
MULTI      DB.View / DB.Update    Starts a transaction.
UNWATCH    —                      Clears watched keys.
WATCH      —                      Watches keys for changes before EXEC.
```

Unlike Redis, Redka's transactions are fully ACID, providing automatic rollback in case of failure.
If a key watched with `WATCH` changes before `EXEC`, Redka discards the queued commands and returns a null array.
