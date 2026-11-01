---
title: Home
description: What's on Home, how Continue watching works, and the rows Matinee builds for you.
sidebar:
  order: 1
---

Home is where Matinee opens. It's built for the person (or [profile](/docs/household/profiles/)) watching, so everyone in the house sees their own.

![Home with a title featured at the top and Continue watching below](../images/home.webp)

## The big picture at the top

The top of Home features the first thing in your **Continue watching**, with how much is left and a **Resume** button. If you have nothing in progress, it features something just added instead. The **×** next to the buttons takes the title off Continue watching (your place is kept for when you come back to it).

## The rows

Scroll down for rows of posters. Each row scrolls sideways, and a row with a **›** next to its name opens as a full page.

![Further down Home: Continue watching, Recently added and New releases](../images/home-rows.webp)

| Row | What's in it |
| --- | --- |
| **Continue watching** | Movies you've started and the next episode of shows you're watching, most recent first. Finishing an episode puts the next one here. |
| **Recently added** | The newest titles in your libraries. A new episode brings its show to the front. |
| **New releases** | Titles in your libraries that came out recently. |
| **Watch list** | What you've saved to [watch later](/docs/watching/watch-list/). |
| **Pinned collections** | Any [collection](/docs/watching/collections/) pinned to Home, such as "Short enough for tonight". |
| **Genres** | A few genres from your library, different each day. |
<!-- since: v0.1.7 -->
| **New episodes** | Episodes from the last two weeks of the [podcasts](/docs/watching/podcasts/) you follow that you haven't started. |
<!-- /since -->
| **Coming soon** | With [Radarr or Sonarr](/docs/server/radarr-and-sonarr/) connected: new episodes and movies due in the next two months. |

<!-- since: v0.1.11 -->
The genre and holiday rows are movies only, so a TV show never turns up among the films. For a row that mixes the two, make a [smart collection](/docs/watching/collections/#make-a-smart-collection) with **Include TV shows** ticked and pin it to Home.
<!-- /since -->

<!-- since: v0.1.1 -->
## See more

A row only has room for so many titles. When there are more, the row ends with a **See more** card: scroll to the end of the row and pick it for a page with every title in the row. It's on Home's rows, on **More like this** under a title, on collections pinned to a library, and on [Discover](/docs/watching/discover/)'s rows, where the page keeps loading more as you scroll. The phone and TV apps have it too.
<!-- /since -->

## Removing something from Continue watching

Hover over a card in **Continue watching** and click **×**. It disappears from the row but keeps its place, so playing it again picks up where you stopped.

<!-- since: v0.1.1 -->
## Holiday rows

In the month before Valentine's Day, Easter, Halloween, Thanksgiving and Christmas, and the week before New Year's Eve, Home leads with a row for the occasion, picked from your library by keywords, titles and descriptions. Halloween fills up with horror and Valentine's Day with romance when there aren't enough matches. An admin can turn holiday rows off under **Settings → Content → Home**.

![The Holiday rows setting](../images/holiday-rows.webp)
<!-- /since -->
