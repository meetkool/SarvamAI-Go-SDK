package main

import (
	"bytes"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
)

func TestFullTurnSendsTranscriptCaptionsAndAudioToTheBrowser(t *testing.T) {
	h := newHarness(t)
	h.fake.reply("what is the weather", "Hello there.", " How are you?")

	stt := h.stt()
	h.waitState("listening", 1)

	frame := micFrame(640)
	h.sendMic(frame)
	if got := stt.waitAudio(t); !bytes.Equal(got, frame) {
		t.Fatalf("Sarvam received %d mic bytes, the browser sent %d: the mic pump must forward every frame byte for byte",
			len(got), len(frame))
	}

	stt.partial(t, "what is the")
	stt.final(t, "what is the weather", models.LangEnglish)

	tr := h.waitState("listening", 2)

	if got, want := tr.texts("partial"), []string{"what is the"}; !slices.Equal(got, want) {
		t.Errorf("partial captions = %q, want %q: the browser shows these while the user is still talking", got, want)
	}
	finals := tr.events("final")
	if len(finals) != 1 || finals[0].Text != "what is the weather" || finals[0].Language != string(models.LangEnglish) {
		t.Errorf("final transcript events = %+v, want one for %q in en-IN", finals, "what is the weather")
	}
	if got, want := tr.texts("reply"), []string{"Hello there.", " How are you?"}; !slices.Equal(got, want) {
		t.Errorf("reply captions = %q, want %q: each sentence is captioned as it is handed to the voice", got, want)
	}
	if got, want := tr.heard(), "Hello there. How are you?"; got != want {
		t.Errorf("the browser was sent %q of audio, want %q: every byte the voice produces has to reach the speaker", got, want)
	}
	if got, want := tr.states(), []string{"listening", "thinking", "speaking", "listening"}; !slices.Equal(got, want) {
		t.Errorf("state events = %q, want %q: the orb in the browser is driven entirely by this sequence", got, want)
	}
	if got := tr.count("interrupt"); got != 1 {
		t.Errorf("the browser got %d interrupt events, want 1: a turn starts by telling the player to drop stale audio", got)
	}

	call := h.fake.waitChat(t)
	if got, want := call.roles(), []string{"system", "user"}; !slices.Equal(got, want) {
		t.Errorf("chat request roles = %q, want %q", got, want)
	}
	if call.Messages[0].Content != systemPrompt {
		t.Errorf("the chat request did not carry the voice agent system prompt")
	}
	if call.Messages[1].Content != "what is the weather" {
		t.Errorf("chat request user message = %q, want the transcript", call.Messages[1].Content)
	}
	if call.Model != string(models.ChatSarvam105BConversations) {
		t.Errorf("chat model = %q, want %q", call.Model, models.ChatSarvam105BConversations)
	}

	query := stt.query
	for name, want := range map[string]string{
		"model":       string(models.STTSaarasV3Realtime),
		"encoding":    string(models.CodecPCM16),
		"sample_rate": "16000",
		"stream_type": string(models.StreamFast),
	} {
		if got := query.Get(name); got != want {
			t.Errorf("transcription query %s = %q, want %q: the browser records 16 kHz PCM16", name, got, want)
		}
	}

	tts := h.fake.waitTTS(t)
	if tts.config.Speaker != testVoice {
		t.Errorf("speech config speaker = %q, want %q", tts.config.Speaker, testVoice)
	}
	if tts.config.Language != string(models.LangEnglish) {
		t.Errorf("speech config language = %q, want the language the user spoke", tts.config.Language)
	}
	if tts.config.SampleRate != "24000" || tts.config.Codec != string(models.CodecPCM16) {
		t.Errorf("speech config audio = %q %q, want 24000 linear16: the browser player assumes 24 kHz PCM16",
			tts.config.SampleRate, tts.config.Codec)
	}
}

func TestASecondTranscriptNeverStartsATurnWhileTheFirstIsStillSpeaking(t *testing.T) {
	h := newHarness(t)

	release := make(chan struct{})
	h.fake.replyFunc("tell me about alpha", func(emit func(string), cancelled <-chan struct{}) {
		emit("Alpha one.")
		select {
		case <-release:
		case <-cancelled:
		case <-time.After(waitTimeout):
		}
		emit(" Alpha two.")
	})
	h.fake.reply("now tell me about beta", "Beta one.", " Beta two.")

	stt := h.stt()
	h.waitState("listening", 1)

	stt.final(t, "tell me about alpha", models.LangEnglish)
	h.waitHeard("Alpha one.")
	alpha := h.fake.waitTTS(t)

	stt.final(t, "now tell me about beta", models.LangEnglish)
	close(release)

	h.waitHeard("Beta one. Beta two.")
	tr := h.waitState("listening", 2)
	beta := h.fake.waitTTS(t)

	for _, text := range alpha.spoken() {
		if strings.Contains(text, "Beta") {
			t.Errorf("the first turn's voice socket was sent %q: the second turn spoke over the first", text)
		}
	}
	for _, text := range beta.spoken() {
		if strings.Contains(text, "Alpha") {
			t.Errorf("the second turn's voice socket was sent %q: the first turn spoke over the second", text)
		}
	}

	heard := tr.heard()
	if last, first := strings.LastIndex(heard, "Alpha"), strings.Index(heard, "Beta"); last > first {
		t.Errorf("the browser was sent %q: audio from the two turns is interleaved, which is two voices at once", heard)
	}

	first, second := h.fake.waitChat(t), h.fake.waitChat(t)
	if first.lastUser() != "tell me about alpha" || second.lastUser() != "now tell me about beta" {
		t.Errorf("chat requests were for %q then %q, want them in the order the user spoke",
			first.lastUser(), second.lastUser())
	}
	if n := h.fake.chatCount(); n != 0 {
		t.Errorf("%d extra chat requests were made: two transcripts must produce at most two turns", n)
	}
	if !slices.Contains(second.roles(), "assistant") {
		t.Errorf("the second turn's messages were %q, want the first turn's reply in the history before it", second.roles())
	}
}

func TestTwoTranscriptsInQuickSuccessionNeverProduceTwoRepliesAtOnce(t *testing.T) {
	h := newHarness(t)
	h.fake.reply("first question", "Alpha one.", " Alpha two.")
	h.fake.reply("second question", "Beta one.", " Beta two.")

	stt := h.stt()
	h.waitState("listening", 1)

	stt.final(t, "first question", models.LangEnglish)
	stt.final(t, "second question", models.LangEnglish)

	h.waitHeard("Beta one. Beta two.")
	tr := h.waitState("listening", 2)

	heard := tr.heard()
	if last, first := strings.LastIndex(heard, "Alpha"), strings.Index(heard, "Beta"); last > first {
		t.Errorf("the browser was sent %q: the interrupted turn kept talking underneath the new one", heard)
	}

	for i, tts := range h.fake.allTTS() {
		texts := strings.Join(tts.spoken(), "")
		if firstBeta := strings.Index(texts, "Beta"); firstBeta >= 0 && strings.LastIndex(texts, "Alpha") > firstBeta {
			t.Errorf("voice socket %d interleaved replies (%q)", i, texts)
		}
	}

	var calls []chatCall
	for h.fake.chatCount() > 0 {
		calls = append(calls, h.fake.waitChat(t))
	}
	if len(calls) == 0 || len(calls) > 2 {
		t.Fatalf("two transcripts produced %d chat requests, want one or two", len(calls))
	}
	if got := calls[len(calls)-1].lastUser(); got != "second question" {
		t.Errorf("the last chat request was for %q, want the newest transcript %q", got, "second question")
	}
}

func TestASingleWordTranscriptIsIgnoredWhileTheAgentIsSpeaking(t *testing.T) {
	gate := make(chan struct{})
	h := newHarness(t, withTTSPolicy(func(flush int) ttsAction {
		if flush > 1 {
			<-gate
		}
		return ttsSpeak
	}))
	h.fake.reply("tell me a story", "Alpha one.", " Alpha two.", " Alpha three.")
	h.fake.reply("please carry on", "Beta one.")

	stt := h.stt()
	h.waitState("listening", 1)

	stt.final(t, "tell me a story", models.LangEnglish)
	h.waitHeard("Alpha one.")
	h.waitState("speaking", 1)

	stt.final(t, "haan", models.LangEnglish)
	stt.final(t, "please carry on", models.LangEnglish)

	tr := h.wait("the browser to be told about the transcript that is not a backchannel", func(tr transcript) bool {
		return slices.Contains(tr.texts("final"), "please carry on")
	})
	if slices.Contains(tr.texts("final"), "haan") {
		t.Errorf("final transcripts shown to the browser = %q: a one word transcript while the agent talks is the user saying mm-hmm, not a new question",
			tr.texts("final"))
	}

	close(gate)
	h.waitHeard("Beta one.")

	for h.fake.chatCount() > 0 {
		if user := h.fake.waitChat(t).lastUser(); user == "haan" {
			t.Errorf("a turn was started for the backchannel %q, which cuts the agent off mid sentence", user)
		}
	}
}

func TestASingleWordTranscriptStartsATurnWhenTheAgentIsIdle(t *testing.T) {
	h := newHarness(t)
	h.fake.reply("haan", "Boliye.")

	stt := h.stt()
	h.waitState("listening", 1)

	stt.final(t, "haan", models.LangEnglish)
	tr := h.waitState("listening", 2)

	if got, want := tr.texts("final"), []string{"haan"}; !slices.Equal(got, want) {
		t.Errorf("final transcripts = %q, want %q: one word answers are real questions when nobody is talking over them", got, want)
	}
	if got, want := tr.heard(), "Boliye."; got != want {
		t.Errorf("the browser was sent %q of audio, want %q", got, want)
	}
	if got := h.fake.waitChat(t).lastUser(); got != "haan" {
		t.Errorf("chat request user message = %q, want %q", got, "haan")
	}
}

func TestSpeechStartWhileSpeakingInterruptsTheTurn(t *testing.T) {
	gate := make(chan struct{})
	h := newHarness(t, withTTSPolicy(func(flush int) ttsAction {
		if flush > 1 {
			<-gate
		}
		return ttsSpeak
	}))
	h.fake.reply("tell me a story", "Alpha one.", " Alpha two.", " Alpha three.")
	h.fake.reply("what did you say", "Beta one.")

	stt := h.stt()
	h.waitState("listening", 1)

	stt.final(t, "tell me a story", models.LangEnglish)
	h.waitHeard("Alpha one.")
	h.waitState("speaking", 1)

	before := h.snapshot().count("interrupt")
	stt.speechStart(t)
	h.waitEvent("interrupt", before+1)

	close(gate)
	stt.final(t, "what did you say", models.LangEnglish)
	h.waitHeard("Beta one.")
	tr := h.waitState("listening", 2)

	heard := tr.heard()
	if strings.Contains(heard, "Alpha two") || strings.Contains(heard, "Alpha three") {
		t.Errorf("the browser was sent %q: audio generated before the barge-in was still forwarded, so the agent talks over the user",
			heard)
	}
	if got, want := heard, "Alpha one.Beta one."; got != want {
		t.Errorf("the browser was sent %q of audio, want %q: only the sentence that was already playing, then the new turn", got, want)
	}
}

func TestSpeechStartWhileIdleLeavesTheTurnAlone(t *testing.T) {
	h := newHarness(t)
	h.fake.reply("hello there", "Sure thing.")

	stt := h.stt()
	h.waitState("listening", 1)

	stt.speechStart(t)
	stt.final(t, "hello there", models.LangEnglish)
	tr := h.waitState("listening", 2)

	if got := tr.count("interrupt"); got != 1 {
		t.Errorf("the browser got %d interrupt events, want only the one that opens a turn: a speech_start while the agent is silent has nothing to interrupt",
			got)
	}
	if got, want := tr.heard(), "Sure thing."; got != want {
		t.Errorf("the browser was sent %q of audio, want %q", got, want)
	}
}

func TestHistoryStoresOnlyTheSpeechTheServiceConfirmed(t *testing.T) {
	h := newHarness(t, withTTSPolicy(func(flush int) ttsAction {
		if flush == 1 {
			return ttsSpeak
		}
		return ttsHangUp
	}))
	h.fake.reply("tell me a story", "Alpha one.", " Alpha two.", " Alpha three.")
	h.fake.reply("what did you say", "Beta one.")

	stt := h.stt()
	h.waitState("listening", 1)

	stt.final(t, "tell me a story", models.LangEnglish)
	h.waitHeard("Alpha one.")
	h.waitState("listening", 2)

	stt.final(t, "what did you say", models.LangEnglish)
	h.fake.waitChat(t)
	second := h.fake.waitChat(t)

	if got, want := second.roles(), []string{"system", "user", "assistant", "user"}; !slices.Equal(got, want) {
		t.Fatalf("the second turn was given roles %q, want %q\nmessages: %+v", got, want, second.Messages)
	}
	if got, want := second.Messages[2].Content, "Alpha one."; got != want {
		t.Errorf("history remembers the agent saying %q, want %q: only the speech the voice service confirmed was spoken belongs in the history, never the whole generated reply",
			got, want)
	}
	if got, want := second.Messages[1].Content, "tell me a story"; got != want {
		t.Errorf("history remembers the user saying %q, want %q", got, want)
	}
	if got, want := second.Messages[3].Content, "what did you say"; got != want {
		t.Errorf("the newest user message is %q, want %q", got, want)
	}
}

func TestTheReplyStillReachesTheBrowserWhenNoAudioArrives(t *testing.T) {
	h := newHarness(t, withTTSPolicy(func(flush int) ttsAction { return ttsHangUp }))
	h.fake.reply("say something", "Nothing to hear.")

	stt := h.stt()
	h.waitState("listening", 1)

	stt.final(t, "say something", models.LangEnglish)
	tr := h.waitState("listening", 2)

	if n := len(tr.audio()); n != 0 {
		t.Fatalf("the voice socket hung up yet %d bytes of audio reached the browser", n)
	}
	if !slices.Contains(tr.texts("reply"), "Nothing to hear.") {
		t.Errorf("reply captions = %q, want the reply text: when the voice fails the user must still be able to read the answer",
			tr.texts("reply"))
	}
	if got := tr.count("error"); got != 1 {
		t.Errorf("the browser was shown %d error events; missing audio should be explained once", got)
	}
	if got := strings.Join(tr.texts("reply"), ""); got != "Nothing to hear." {
		t.Errorf("caption duplicated after audio failure: %q", got)
	}
}

func TestSpeechRecoversAfterAConnectionFails(t *testing.T) {
	h := newHarness(t, withTTSPolicy(func(int) ttsAction { return ttsHangUp }))
	h.fake.reply("first question", "First answer.")
	h.fake.reply("second question", "Second answer.")
	stt := h.stt()
	h.waitState("listening", 1)
	stt.final(t, "first question", models.LangEnglish)
	h.waitState("listening", 2)
	h.fake.setTTSPolicy(func(int) ttsAction { return ttsSpeak })
	stt.final(t, "second question", models.LangEnglish)
	tr := h.waitState("listening", 3)
	if got := tr.heard(); got != "Second answer." {
		t.Fatalf("audio did not recover: %q", got)
	}
	if len(h.fake.allTTS()) != 2 {
		t.Fatal("failed voice connection was not replaced")
	}
}

func TestInterruptUnblocksSpeechThatNeverFinishes(t *testing.T) {
	h := newHarness(t, withTTSPolicy(func(flush int) ttsAction {
		if flush > 1 {
			return ttsStall
		}
		return ttsSpeak
	}))
	h.fake.reply("tell a story", "First sentence.", " Second sentence.")
	h.fake.reply("new question", "Recovered.")
	stt := h.stt()
	h.waitState("listening", 1)
	stt.final(t, "tell a story", models.LangEnglish)
	h.waitHeard("First sentence.")
	stt.speechStart(t)
	h.fake.setTTSPolicy(func(int) ttsAction { return ttsSpeak })
	stt.final(t, "new question", models.LangEnglish)
	h.waitHeard("Recovered.")
}

func TestTheSpeakerSocketIsReusedUntilTheLanguageChanges(t *testing.T) {
	h := newHarness(t)
	h.fake.reply("first english question", "One.")
	h.fake.reply("second english question", "Two.")
	h.fake.reply("hindi question", "Teen.")

	stt := h.stt()
	h.waitState("listening", 1)

	stt.final(t, "first english question", models.LangEnglish)
	h.waitState("listening", 2)
	stt.final(t, "second english question", models.LangEnglish)
	h.waitState("listening", 3)

	english := h.fake.waitTTS(t)
	if n := len(h.fake.tts); n != 0 {
		t.Errorf("%d extra speech sockets were opened for a second English turn: reopening one costs a second of silence before every reply", n)
	}
	if got, want := english.spoken(), []string{"One.", "Two."}; !slices.Equal(got, want) {
		t.Errorf("the reused speech socket was sent %q, want %q", got, want)
	}

	stt.final(t, "hindi question", models.LangHindi)
	h.waitState("listening", 4)

	hindi := h.fake.waitTTS(t)
	if got, want := hindi.config.Language, string(models.LangHindi); got != want {
		t.Errorf("the new speech socket was opened for %q, want %q: a change of language needs a new voice session", got, want)
	}
	if got, want := hindi.spoken(), []string{"Teen."}; !slices.Equal(got, want) {
		t.Errorf("the Hindi speech socket was sent %q, want %q", got, want)
	}
}

func TestBlankTranscriptsStartNoTurn(t *testing.T) {
	h := newHarness(t)
	h.fake.reply("a real question", "Of course.")

	stt := h.stt()
	h.waitState("listening", 1)

	stt.final(t, "", models.LangEnglish)
	stt.final(t, "   \t ", models.LangEnglish)
	stt.final(t, "a real question", models.LangEnglish)

	tr := h.waitState("listening", 2)

	if got, want := tr.texts("final"), []string{"a real question"}; !slices.Equal(got, want) {
		t.Errorf("final transcripts = %q, want %q: silence must not be sent to the model", got, want)
	}
	if got := h.fake.waitChat(t).lastUser(); got != "a real question" {
		t.Errorf("chat request user message = %q, want %q", got, "a real question")
	}
	if n := h.fake.chatCount(); n != 0 {
		t.Errorf("%d extra chat requests were made for blank transcripts", n)
	}
}

func TestTheBrowserIsToldWhenSarvamRefusesTheTranscriptionSocket(t *testing.T) {
	h := newHarness(t, withTranscriptionRefused(http.StatusUnauthorized))

	tr := h.waitEvent("error", 1)
	if got := tr.texts("error")[0]; got == "" {
		t.Errorf("the browser was sent an empty error event; the user has to be told why the mic is dead")
	}
	if got := tr.countState("listening"); got != 0 {
		t.Errorf("the browser was told it is listening %d time(s) even though transcription never opened", got)
	}

	select {
	case <-h.done:
	case <-time.After(waitTimeout):
		t.Fatal("the session hung after Sarvam refused the transcription socket")
	}
	if h.sessionError() == nil {
		t.Errorf("the session reported success even though transcription was refused")
	}
}

func TestTTSLanguageFallsBackToEnglishForVoicesBulbulCannotSpeak(t *testing.T) {
	for _, tc := range []struct {
		spoken models.Language
		want   models.Language
	}{
		{models.LangEnglish, models.LangEnglish},
		{models.LangHindi, models.LangHindi},
		{models.LangBengali, models.LangBengali},
		{models.LangGujarati, models.LangGujarati},
		{models.LangKannada, models.LangKannada},
		{models.LangMalayalam, models.LangMalayalam},
		{models.LangMarathi, models.LangMarathi},
		{models.LangOdia, models.LangOdia},
		{models.LangPunjabi, models.LangPunjabi},
		{models.LangTamil, models.LangTamil},
		{models.LangTelugu, models.LangTelugu},
		{"or-IN", models.LangOdia},
		{"ur-IN", models.LangEnglish},
		{"", models.LangEnglish},
		{"nonsense", models.LangEnglish},
	} {
		if got := ttsLanguage(tc.spoken); got != tc.want {
			t.Errorf("ttsLanguage(%q) = %q, want %q: an unsupported language must still produce a reply, not a dead turn",
				tc.spoken, got, tc.want)
		}
	}
}
