package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheRunReportSaysWhyEachCatchUpEnded(t *testing.T) {
	var out bytes.Buffer
	writeCatchUp(&out, &CatchUpCount{Stored: 40, Endings: &CatchUpEndings{
		CaughtUp: 20, NeverGranted: 2, ToldToWait: 3, ConnectionEnded: 1, RunEnded: 4,
		ToldToWaitTimes: map[int]int64{9: 2, 2: 1},
	}})

	assert.Contains(t, out.String(),
		"Catch-up ended: 20 caught up, 2 never granted a turn, 3 told to wait (2 times: 1, 9 times: 2), "+
			"1 on a broken connection, 4 with the run; 0 turns asked again")
}

func TestEveryMachineThatCaughtUpIsCounted(t *testing.T) {
	tally := &catchUpTally{}
	tally.ended("contoso-db-01", catchUpEnd{why: endedCaughtUp})
	tally.ended("contoso-web-02", catchUpEnd{why: endedCaughtUp})

	assert.EqualValues(t, 2, tally.count().Complete)
}

func TestAMachineThatCaughtUpStaysCaughtUpWhenALaterConnectionBreaks(t *testing.T) {
	tally := &catchUpTally{}
	tally.ended("contoso-db-01", catchUpEnd{why: endedCaughtUp})
	tally.ended("contoso-db-01", catchUpEnd{why: endedConnection})

	count := tally.count()
	assert.EqualValues(t, 1, count.Complete)
	assert.Equal(t, &CatchUpEndings{CaughtUp: 1}, count.Endings)
}

func TestAMachineThatNeverCaughtUpIsCountedByItsLastEnding(t *testing.T) {
	tally := &catchUpTally{}
	tally.ended("contoso-db-01", catchUpEnd{why: endedNeverGranted})
	tally.ended("contoso-db-01", catchUpEnd{why: endedToldToWait, waits: 9})

	count := tally.count()
	assert.Zero(t, count.Complete)
	assert.Equal(t, &CatchUpEndings{ToldToWait: 1, ToldToWaitTimes: map[int]int64{9: 1}}, count.Endings)
}

func TestARunThatSentNoCatchUpReportsNone(t *testing.T) {
	assert.Nil(t, (&catchUpTally{}).count(), "nothing sent differs from nothing stored")
	var none *catchUpTally
	assert.Nil(t, none.count())
}

func TestTheRunReportSaysHowCatchUpWasAnswered(t *testing.T) {
	var out bytes.Buffer
	writeCatchUp(&out, &CatchUpCount{Stored: 40, Deferred: 3, Unanswered: 1})
	assert.Contains(t, out.String(), "Catch-up:    40 stored, 3 told to wait, 1 never answered")

	out.Reset()
	writeCatchUp(&out, nil)
	assert.Empty(t, out.String(), "a run that sent no catch-up reports none")
}
