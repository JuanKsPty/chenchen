-- One purchase, however many ways it arrives (ADR-0002). A tap with Apple Pay
-- can reach chenchén twice: the phone's shortcut sends it the moment it
-- happens, and the bank's email says the same a minute later. Each is a
-- signal; the purchase is the capture they both tell of, and it counts once.
--
-- A capture has at least one signal, the channel that noted it first, and
-- never two of the same channel: a second signal of a channel it already has
-- is another purchase. captures.JoinSignal decides which capture a new signal
-- tells of before anything is noted; when none, the capture is noted with the
-- signal as its first. Joining never changes the amount, day or state of the
-- capture it joins: a linked or a discarded purchase stays so, and never counts
-- again. A purchase typed, or noted by the assistant, only fills in the note
-- and the category it lacks, and never joins a discarded one.
--
-- The shortcut's own duplicate rule (the same merchant within two minutes)
-- reads its signals, so a tap the shortcut sent twice is one whether its first
-- copy noted a purchase or joined one.
--
-- 'agent' is the assistant noting a purchase it read while the owner's
-- statement is still in transit. Like a purchase typed by hand, its time is
-- when it was noted, not when the purchase happened.

-- +goose Up
CREATE TABLE capture_signals (
    capture_id UUID        NOT NULL,
    user_id    UUID        NOT NULL,
    channel    TEXT        NOT NULL
               CHECK (channel IN ('shortcut', 'manual', 'agent', 'email', 'notification')),
    -- When the channel heard of the purchase. For the shortcut, an email or a
    -- notification that is the purchase's own time; typed or noted by the
    -- assistant, only when it was noted.
    at         TIMESTAMPTZ NOT NULL,
    -- The bank alert this signal was read from, once alerts are kept.
    alert_id   UUID,
    -- What the channel sent, as it sent it.
    raw        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- The id a caller chose for a new purchase (captures.AddInput.ID) when
    -- its signal joined one already noted instead: a repeat of that call
    -- finds the purchase by it rather than note it after all. An id names one
    -- purchase of the user's, so it is unique per user.
    caller_id  UUID,
    PRIMARY KEY (capture_id, channel),
    FOREIGN KEY (capture_id, user_id) REFERENCES captures (id, user_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX capture_signals_caller_idx ON capture_signals (user_id, caller_id)
    WHERE caller_id IS NOT NULL;

-- Every capture so far arrived one way: that way is its first signal.
INSERT INTO capture_signals (capture_id, user_id, channel, at, raw)
SELECT id, user_id, source, received_at, raw FROM captures;

ALTER TABLE captures DROP CONSTRAINT captures_source_check; -- 00014's inline CHECK
ALTER TABLE captures ADD CONSTRAINT captures_source_check CHECK (source IN ('shortcut', 'manual', 'agent'));

-- +goose Down
-- A purchase the assistant noted first that the shortcut or the owner's hand
-- told of too is theirs again: the first of those becomes the channel that
-- noted it, when and with what it sent, as it would have been without the
-- assistant. Only a purchase the assistant alone told of cannot exist without
-- 'agent': it goes, and its signals with it.
UPDATE captures c SET source = s.channel, received_at = s.at, raw = s.raw
  FROM (SELECT DISTINCT ON (capture_id) capture_id, channel, at, raw
          FROM capture_signals WHERE channel IN ('shortcut', 'manual')
         ORDER BY capture_id, at, channel) s
 WHERE c.id = s.capture_id AND c.source = 'agent';
DELETE FROM captures WHERE source = 'agent';
ALTER TABLE captures DROP CONSTRAINT captures_source_check;
ALTER TABLE captures ADD CONSTRAINT captures_source_check CHECK (source IN ('shortcut', 'manual'));
DROP TABLE IF EXISTS capture_signals;
