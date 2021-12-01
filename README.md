NNS Proposals Bot
=================

A [telegram bot](https://t.me/NNSProposalsBot) alerting about all new NNS proposals.
Supports filtering on the proposal topic.

## Compilation & Execution

Compile the bot using `go build`; this will create an executable `nns-proposals-bot`.

## Proposer Whitelisting

In case certain proposers should be ignored, save the proposer ids into the file `proposer_whitelist.txt`, one per line.
The bot will use this file automatically if it can be found inside the same directory as the executable.

## Interaction with the bot

Enter `/start` to subscribe to the notifications; use `/stop` to cancel the subscription.
Use `/block` or `/unblock` to block or unblock proposals with a certain topic; use `/blacklist` to display the list of blocked topics.
