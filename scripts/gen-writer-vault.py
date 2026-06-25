#!/usr/bin/env python3
"""Generate a synthetic Markdown writer vault for the QA workspace.

Creates an Obsidian-style vault with:
  - Daily notes with dates and tags
  - Interview transcripts with frontmatter
  - Research notes with wikilinks
  - Draft articles with revision markers
  - Published pieces with metadata

Usage: python3 scripts/gen-writer-vault.py <dest_dir>

The vault structure matches the labeled_queries.yaml writer persona
judgments (q021-q028).
"""

import os
import sys
from pathlib import Path

VAULT_STRUCTURE = {
    "interviews": {
        "dr-chen-renewable-energy.md": """---
title: Interview — Dr. Chen on Renewable Energy
date: 2024-02-20
tags: [interview, energy, climate]
interviewee: Dr. Sarah Chen
---

# Interview: Dr. Chen on Renewable Energy Transition

**Date:** February 20, 2024
**Location:** Stanford Energy Institute

## Key Points

- Solar panel efficiency has improved 40% in last 5 years
- Battery storage costs dropping faster than projections
- Grid integration remains the primary challenge
- Policy support is essential for transition timeline

## Full Transcript

**Q: How has solar efficiency changed recently?**

A: We've seen remarkable improvements. Five years ago, commercial panels
were around 18-20% efficient. Today, the best monocrystalline panels
exceed 25%, and lab results are pushing 30%. The cost per watt has
dropped correspondingly.

**Q: What about storage?**

A: Lithium-ion battery costs have fallen below $150/kWh, which was our
threshold for grid-scale viability. But the real game-changer is
emerging solid-state technology — potentially safer and more energy-dense.
""",
        "housing-crisis-tenant-a.md": """---
title: Interview — Housing Crisis Series — Tenant A
date: 2024-01-15
tags: [interview, housing, series]
series: housing-crisis
---

# Interview: Housing Crisis — Tenant Perspective

**Date:** January 15, 2024

## Background

Maria (name changed) has lived in her rent-controlled apartment for 12 years.
She's facing a 40% rent increase after the building was sold to a new owner.

## Key Quotes

> "I've been here twelve years. My kids grew up here. And now they want
> us out so they can renovate and charge three times the rent."

> "The city says there are protections, but when the lawyers come,
> what can we do?"
""",
        "housing-crisis-developer-b.md": """---
title: Interview — Housing Crisis Series — Developer B
date: 2024-01-22
tags: [interview, housing, series]
series: housing-crisis
---

# Interview: Housing Crisis — Developer Perspective

**Date:** January 22, 2024

## Background

James is a mid-scale developer who builds workforce housing. He's facing
increasing construction costs and regulatory hurdles.

## Key Quotes

> "Everyone wants affordable housing, but nobody wants it next to them.
> And the permitting process adds 18 months and $50K per unit."

> "I'd love to build more workforce housing, but the math doesn't work
> unless the city provides density bonuses or tax incentives."
""",
    },
    "research": {
        "energy-transition-notes.md": """---
title: Energy Transition Research Notes
tags: [research, energy, climate-change]
updated: 2024-03-01
---

# Energy Transition Research

## Sources
- [[dr-chen-renewable-energy]] — key interview on solar and battery tech
- IEA World Energy Outlook 2024
- IPCC AR6 Working Group III

## Key Statistics
- Global renewable capacity: 3,372 GW (2023)
- Solar: fastest-growing energy source for 3rd consecutive year
- Wind: 10% of global electricity generation

## Notes
The transition is happening faster than most models predicted in 2020,
but distribution is uneven. OECD countries lead in deployment; developing
nations face financing gaps.

Climate change tagged content for cross-reference with daily notes.
""",
        "water-policy-sources.md": """---
title: Water Rights — Source List
tags: [research, water, policy]
updated: 2024-02-28
---

# Water Rights Investigation — Sources

## Primary Sources
- State Water Resources Control Board records
- County assessor data on agricultural water rights
- Court filings: Valley Water District v. Agricultural Association

## Expert Contacts
- Prof. James Rodriguez, UC Davis Water Policy
- Sarah Kim, State Water Board (background only)
- Mike Chen, Valley Farmers Cooperative
""",
        "tech-industry-sources.md": """---
title: Tech Layoffs — Research Sources
tags: [research, tech, layoffs]
updated: 2024-02-15
---

# Tech Layoffs Analysis — Sources

## Data Sources
- layoffs.fyi tracker (public dataset)
- SEC filings (10-K, 10-Q, 8-K for affected companies)
- Bureau of Labor Statistics JOLTS data

## Interview Sources
- 3 anonymous HR directors at FAANG companies
- 2 affected engineering managers
- 1 labor economist (quoted by name)
""",
    },
    "drafts": {
        "water-rights-investigation.md": """---
title: "DRAFT: The Water Rights Nobody Talks About"
status: draft-v2
tags: [draft, water, investigation]
word_count: 2400
target_publication: "City Journal"
---

# The Water Rights Nobody Talks About

*Draft v2 — needs fact-check on agricultural allocation numbers*

## Lede

In the driest year on record, three corporate farms in the Central Valley
hold water rights that predate the state's modern allocation system by
80 years. While cities impose mandatory rationing, these farms irrigate
almonds — one of the thirstiest crops — using rights granted in 1914.

## Section 1: The Historical Rights

[TODO: verify dates with county records]

The prior appropriation doctrine — "first in time, first in right" —
gives senior water rights holders priority over junior holders and cities.
See [[water-policy-sources]] for source list.
""",
        "housing-crisis-series-outline.md": """---
title: Housing Crisis Series — Outline
status: planning
tags: [outline, housing, series]
parts: 4
---

# Housing Crisis Series — Outline

## Part 1: The Tenant's Story
- Maria's 12-year tenancy threatened
- See [[housing-crisis-tenant-a]]

## Part 2: The Developer's Dilemma
- Construction costs vs. affordability mandates
- See [[housing-crisis-developer-b]]

## Part 3: The Policy Gap
- What city council promised vs. delivered
- [TODO: interview council member]

## Part 4: A Way Forward
- Models from other cities
- [TODO: research Vienna, Singapore, Tokyo]
""",
        "book-proposal-v2.md": """---
title: Book Proposal — The Divided Block
status: draft
tags: [draft, book, proposal]
---

# The Divided Block: A Book Proposal

## Overview
A narrative nonfiction account of one city block's transformation over
three decades — from working-class neighborhood to contested ground in
the housing crisis.

## Target Audience
General nonfiction readers interested in urbanism, housing policy,
and narrative journalism. Comp titles: Evicted (Matthew Desmond),
Arbitrary Lines (M. Nolan Gray).

## Chapter Outline
1. The Block in 1994
2. The First Wave (2005-2010)
3. The Great Recession's Shadow
4. New Owners, New Rules
5. The Tenants Fight Back
6. What Comes Next
""",
    },
    "published": {
        "tech-layoffs-analysis.md": """---
title: "When the Music Stopped: Inside Tech's Great Contraction"
published: 2024-02-01
publication: "City Journal"
tags: [published, tech, layoffs]
---

# When the Music Stopped: Inside Tech's Great Contraction

*Published February 1, 2024 in City Journal*

The layoff notices came on a Tuesday. Not by email — that would have been
too personal — but by Slack message, sent simultaneously to 1,200 people
across three time zones.

"Your role has been eliminated as part of a company-wide restructuring."

See [[tech-industry-sources]] for full source list.
""",
        "education-budget-cuts.md": """---
title: "School Board Approves Controversial Budget Cuts"
published: 2024-03-01
publication: "Local Tribune"
tags: [published, education, budget]
---

# School Board Approves Controversial Budget Cuts

*Published March 1, 2024 in Local Tribune*

In a 4-3 vote Tuesday night, the school board approved $2.3 million in
budget cuts that will eliminate 15 teaching positions and close two
after-school programs.
""",
    },
    "daily": {
        "2024-02-school-board-meeting.md": """---
date: 2024-02-27
tags: [daily, education, meeting]
---

# 2024-02-27 — School Board Meeting

Attended the school board meeting tonight. Heated debate over proposed
budget cuts. Parents packed the auditorium.

Key takeaway: the board is split 4-3, with the deciding vote coming from
the newly elected member who campaigned on fiscal responsibility.

Follow up: get comment from superintendent for the article.
""",
        "2024-03-11-monday.md": """---
date: 2024-03-11
tags: [daily, conference]
---

# 2024-03-11 Monday

Arrived at the conference hotel. Registration went smoothly.
Ran into Prof. Rodriguez — scheduled informal coffee meeting for Wednesday.

Sessions attended:
- "Data Journalism in the AI Era" (good, need to follow up on tool recommendations)
- "Investigative Techniques for Local Reporting" (mostly review for me)
""",
        "2024-03-12-tuesday.md": """---
date: 2024-03-12
tags: [daily, conference]
---

# 2024-03-12 Tuesday

Full day of sessions. Highlight was the panel on housing reporting —
three journalists from different cities compared approaches.

Key contact: Lisa Park, investigative reporter at Metro Times. She's
done a similar housing series and offered to share her FOIA template.
""",
        "2024-03-15-climate-summit.md": """---
date: 2024-03-15
tags: [daily, conference, climate-change]
---

# 2024-03-15 Friday — Climate Summit Side Event

Attended the climate journalism side event. Good panel on covering
energy transition without falling into false balance.

Met Dr. Chen again — she mentioned new data on battery storage costs
that contradicts some of our earlier reporting. Need to follow up.
See [[dr-chen-renewable-energy]] for prior interview.
""",
    },
    "correspondence": {
        "editor-feedback-march.md": """---
date: 2024-03-05
from: "Jessica Torres, City Journal"
tags: [correspondence, editor, feedback]
---

# Editor Feedback — March 2024

## On "The Divided Block" proposal
Jessica says the proposal is strong but wants:
- Stronger opening anecdote (not the 1994 history — start with present day)
- Clearer comp title positioning
- A sample chapter (Chapter 4 or 5 — the conflict chapters)

## On the water rights piece
- Approved for 3,000 words (up from 2,400)
- Wants the almond farming angle in the lede
- Fact-check deadline: March 20
""",
    },
}


def write_vault(dest_dir: str) -> None:
    """Write the vault structure to the destination directory."""
    dest = Path(dest_dir)
    total_files = 0

    for folder, files in VAULT_STRUCTURE.items():
        folder_path = dest / folder
        folder_path.mkdir(parents=True, exist_ok=True)
        for filename, content in files.items():
            file_path = folder_path / filename
            file_path.write_text(content.strip() + "\n", encoding="utf-8")
            total_files += 1

    print(f"Generated {total_files} vault files in {dest}")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <dest_dir>", file=sys.stderr)
        sys.exit(1)
    write_vault(sys.argv[1])
