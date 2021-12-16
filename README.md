NNS Proposals Bot
=================

A [telegram bot](https://t.me/NNSProposalsBot) alerting about all new NNS proposals.
Supports filtering on the proposal topic.

## Compilation & Execution

Compile the bot using `go build`; this will create an executable `nns-proposals-bot`.
Run the bot with the authentication token provided in the `TOKEN` environment variable:

    TOKEN=<...> ./nns-proposals-bot

## Interaction with the bot

Enter `/start` to subscribe to the notifications; use `/stop` to cancel the subscription.
Use `/block` or `/unblock` to block or unblock proposals with a certain topic; use `/blacklist` to display the list of blocked topics.
