#!/bin/bash

PEM=$1
NEURON=$2
PROPOSAL=$3
VOTE=$4

./qu --pem-file "$PEM" raw manage_neuron "(record{id=opt record{id=$NEURON:nat64};command=opt variant{RegisterVote=record{vote=$VOTE:int32;proposal=opt record{id=$PROPOSAL:nat64}}}})"| ./qu send --yes -
