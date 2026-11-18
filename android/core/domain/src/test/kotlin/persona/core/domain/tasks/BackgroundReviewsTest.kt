package persona.core.domain.tasks

import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.domain.lines.ChatLine

/** #5084 marked a background supervisor's review; #8312 draws no review in the conversation or the background sheet. */
class BackgroundReviewsTest {
    private fun review(id: String, by: String? = "background", verdict: String = "accepted") =
        FeedItem(id, "main", "2026-11-12T10:00:00Z", "review", "t", buildJsonObject {
            put("title", "Fix the login"); put("verdict", verdict); by?.let { put("by", it) }
        })
    private val todos = listOf(Todo("t", "Fix the login", origin = "main", supervision = Supervision(supervisor = "main")))
    private fun feedIds(lines: List<ChatLine>) =
        lines.filterIsInstance<ChatLine.Extra>().mapNotNull { (it.payload as? AssistantExtra.Feed)?.item?.id }

    @Test fun theHubMarksTheBackgroundSupervisorsReviews() {
        assertTrue(AssistantTaskCards.isBackgroundSupervisorReview(review("a")))
        assertTrue(AssistantTaskCards.isBackgroundSupervisorReview(review("b", verdict = "rework")))
        // the conversation's own review, another author, and a hub that does not say who wrote it
        assertFalse(AssistantTaskCards.isBackgroundSupervisorReview(review("c", by = "")))
        assertFalse(AssistantTaskCards.isBackgroundSupervisorReview(review("d", by = "judge")))
        assertFalse(AssistantTaskCards.isBackgroundSupervisorReview(review("e", by = null)))
        // only a review: another entry with the same field is nothing new
        assertFalse(AssistantTaskCards.isBackgroundSupervisorReview(review("f").copy(kind = "receipt")))
        assertFalse(AssistantTaskCards.isBackgroundSupervisorReview(FeedItem("g", "main", "2026-11-12T10:00:00Z", "review", "t")))
    }

    @Test fun aReviewIsNotDrawnAndStaysInTheFeed() {
        val feed = listOf(review("own", by = null), review("background"))
        assertEquals(emptyList<String>(), feedIds(AssistantTaskCards.lines(emptyList(), feed, emptyMap(), todos, inline = true)))
        assertEquals(emptyList<String>(), feedIds(AssistantTaskCards.lines(emptyList(), feed, emptyMap(), todos)))
        assertEquals(emptyList<String>(), AssistantTaskCards.backgroundFeed(feed).map { it.id })
        assertEquals(emptyList<String>(), AssistantTaskCards.backgroundFeed(feed, inline = true).map { it.id })
        assertEquals(listOf("own", "background"), feed.map { it.id })
    }

    @Test fun aHubWithoutTheMarkDrawsNoReviewEither() {
        val feed = listOf(review("one", by = null), review("two", by = null, verdict = "rework"))
        assertEquals(emptyList<String>(), feedIds(AssistantTaskCards.lines(emptyList(), feed, emptyMap(), todos)))
        assertEquals(emptyList<String>(), AssistantTaskCards.backgroundFeed(feed).map { it.id })
        assertEquals(listOf("one", "two"), feed.map { it.id })
    }

    @Test fun itsReviewAlreadyAmongTheLinesStaysInTheForeground() {
        val shown = listOf<ChatLine>(ChatLine.Extra("feed-new", AssistantExtra.Feed(review("new"))))
        assertEquals(shown, AssistantConversation.project(shown, emptyList()).foreground)
        val own = listOf<ChatLine>(ChatLine.Extra("feed-own", AssistantExtra.Feed(review("own", by = null))))
        assertEquals(own, AssistantConversation.project(own, emptyList()).background)
    }
}
