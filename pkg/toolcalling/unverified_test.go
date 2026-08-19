package toolcalling

import "testing"

func TestClaimsUnverifiedCompletionCatchesFirstPersonReports(t *testing.T) {
	claims := []string{
		"I created the file at src/main.go and it compiles.",
		"I've installed the dependency for you.",
		"I ran the tests and they all pass.",
		"I successfully deployed the service.",
		"Done! The configuration is in place.",
		"All set, the migration is applied.",
		"I already updated the README.",
		"I went ahead and removed the stale entries.",
	}
	for _, claim := range claims {
		if !ClaimsUnverifiedCompletion(claim) {
			t.Fatalf("a first-person completion report was missed: %q", claim)
		}
	}
}

func TestClaimsUnverifiedCompletionIgnoresThirdPersonFacts(t *testing.T) {
	// The reference this guard is drawn from matches bare past-tense verbs and
	// rejects answers like these, which are ordinary facts about the world.
	facts := []string{
		"Go was created at Google in 2007.",
		"The file was created in 2019 by the original author.",
		"The migration ran successfully in production last week.",
		"Docker images are built and deployed by the pipeline.",
		"The command executed on the CI runner, not here.",
	}
	for _, fact := range facts {
		if ClaimsUnverifiedCompletion(fact) {
			t.Fatalf("a third-person statement was treated as a completion claim: %q", fact)
		}
	}
}

func TestClaimsUnverifiedCompletionIgnoresHedgedReports(t *testing.T) {
	hedged := []string{
		"I created the patch text below, but I cannot confirm it applies cleanly.",
		"I wrote out the steps; you will need to run them yourself.",
		"I ran into a permission error and could not continue.",
		"I can't execute anything here, so please run the command locally.",
	}
	for _, answer := range hedged {
		if ClaimsUnverifiedCompletion(answer) {
			t.Fatalf("a hedged answer was treated as a completion claim: %q", answer)
		}
	}
}

func TestClaimsUnverifiedCompletionIgnoresLongProse(t *testing.T) {
	// A long answer that happens to say "I created a plan" is the prose the
	// caller asked for; replacing it would destroy real work.
	long := "Here is the migration plan. I created a plan with four phases. "
	for len(long) <= unverifiedClaimMaxLen {
		long += "Each phase lists the tables, the expected downtime and the rollback step. "
	}
	if ClaimsUnverifiedCompletion(long) {
		t.Fatal("a long prose answer was treated as a completion claim")
	}
}

func TestClaimsUnverifiedCompletionIgnoresTheToolBanInstruction(t *testing.T) {
	// The instruction travels inside the request, so a model that echoes it
	// must not trip the detector it was written to prevent.
	if ClaimsUnverifiedCompletion(nativeToolBanInstruction) {
		t.Fatal("the native tool ban instruction trips the completion detector")
	}
	if ClaimsUnverifiedCompletion(RepeatedCallsNotice) {
		t.Fatal("the repeated-calls notice trips the completion detector")
	}
}
