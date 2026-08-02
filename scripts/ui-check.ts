// Checks the configurator in a real browser: drives the actual UI in Chromium
// and asserts the things jsdom cannot see — measured geometry, soft wrapping,
// and layout stability (does hovering or selecting a chip move anything?).
// Screenshots go alongside the results for visual review.
//
// Run it through scripts/ui-check.sh (or `mise run ui-check`), which starts a
// configurator against a THROWAWAY config and passes UI_URL. Never point it at a
// configurator serving your own configuration: it edits the document it is
// given. The runner also wipes the store before each run — a leftover variant
// from a previous run silently invalidated the "an unconditional variant shows
// only the add button" check.
//
// These checks complement the vitest suite rather than replacing it: anything
// expressible in jsdom belongs there, because it runs in seconds and needs no
// browser.

import { chromium, type Browser, type Locator, type Page } from "playwright";
import { mkdirSync, writeFileSync } from "node:fs";

const URL_BASE = process.env.UI_URL;
const OUT = process.env.UI_OUT ?? "./shots";
if (!URL_BASE) {
    throw new Error("UI_URL is required (the configurator URL incl. #token=...)");
}

interface Result {
    name: string;
    ok: boolean;
    detail: string;
}

const results: Result[] = [];

function check(name: string, ok: boolean, detail: string): void {
    results.push({ name, ok, detail });
    console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? ` — ${detail}` : ""}`);
}

async function shot(page: Page, name: string): Promise<void> {
    await page.screenshot({ path: `${OUT}/${name}.png`, fullPage: false });
}

// Rounded bounding box, so sub-pixel noise does not read as movement.
async function box(l: Locator): Promise<{ x: number; y: number; w: number; h: number }> {
    const b = await l.boundingBox();
    if (!b) {
        throw new Error("element has no box");
    }
    return {
        x: Math.round(b.x),
        y: Math.round(b.y),
        w: Math.round(b.width),
        h: Math.round(b.height),
    };
}

async function main(): Promise<void> {
    mkdirSync(OUT, { recursive: true });
    const browser: Browser = await chromium.launch();
    const page = await browser.newPage({ viewport: { width: 1500, height: 1000 } });
    const consoleErrors: string[] = [];
    page.on("console", (m) => {
        if (m.type() === "error") {
            consoleErrors.push(m.text());
        }
    });
    page.on("pageerror", (e) => consoleErrors.push(`pageerror: ${e.message}`));

    await page.goto(URL_BASE!, { waitUntil: "networkidle" });
    await page.waitForSelector('[data-testid="settings-button"]', { timeout: 15000 });
    await page.waitForSelector(".seg-chip", { timeout: 15000 });
    await shot(page, "01-initial");

    // ---- 1. Palette: wide chips leave the two-column grid ----------------
    const chips = page.locator(".palette-chip");
    const chipCount = await chips.count();
    const wide = page.locator(".palette-chip.wide");
    const wideCount = await wide.count();
    // A wide chip must be visibly wider than a normal one (it spans both
    // columns), and no chip may overflow the palette panel.
    const normal = page.locator(".palette-chip:not(.wide)").first();
    const nb = await box(normal);
    const wb = wideCount > 0 ? await box(wide.first()) : null;
    check(
        "palette: wide chips span both columns",
        wideCount > 0 && wb !== null && wb.w > nb.w * 1.6,
        `${wideCount}/${chipCount} wide; normal w=${nb.w}, wide w=${wb?.w}`,
    );

    const panel = page.locator(".side .panel").first();
    const pb = await box(panel);
    let overflowing = 0;
    for (let i = 0; i < chipCount; i += 1) {
        const b = await box(chips.nth(i));
        if (b.x + b.w > pb.x + pb.w + 1) {
            overflowing += 1;
        }
    }
    check("palette: no chip overflows the panel", overflowing === 0, `${overflowing} overflow`);

    // Every chip's sample text must be fully inside its own chip box (the
    // ellipsis case is fine; escaping the chip is not).
    const clipped = await page.evaluate(() => {
        let bad = 0;
        for (const el of document.querySelectorAll<HTMLElement>(".palette-chip")) {
            if (el.scrollWidth > el.clientWidth + 1) {
                bad += 1;
            }
        }
        return bad;
    });
    check("palette: no chip content escapes its box", clipped === 0, `${clipped} clipped`);
    await page.locator(".side").first().screenshot({ path: `${OUT}/02-palette.png` });

    // ---- 2. Row tracks soft-wrap, with a marker -------------------------
    // Fill a line until it has to wrap: click palette chips (they append to the
    // active line).
    const before = await page.locator(".row-track").first().locator(".seg-chip").count();
    for (let i = 0; i < 10; i += 1) {
        await page.locator('[data-testid="palette-field:model"]').click();
        await page.waitForTimeout(120);
    }
    await page.waitForTimeout(800);
    const track = page.locator(".row-track").first();
    const after = await track.locator(".seg-chip").count();
    const rows = await page.evaluate(() => {
        const t = document.querySelector<HTMLElement>(".row-track");
        if (!t) {
            return { visualRows: 0, marks: 0, scrollable: false };
        }
        const tops = new Set<number>();
        for (const c of Array.from(t.children)) {
            if (c instanceof HTMLElement && c.classList.contains("seg-chip")) {
                tops.add(c.offsetTop);
            }
        }
        const surface = document.querySelector<HTMLElement>(".preview-surface");
        return {
            visualRows: tops.size,
            marks: t.querySelectorAll('.seg-chip[data-wrap-end="true"]').length,
            // The whole point of wrapping: the editor must not need horizontal
            // scrolling to reach the chips.
            scrollable: surface ? surface.scrollWidth > surface.clientWidth + 1 : false,
        };
    });
    check(
        "row: a long line wraps onto several visual rows",
        rows.visualRows > 1,
        `${after} chips (was ${before}) on ${rows.visualRows} visual rows`,
    );
    check(
        "row: each wrapped row but the last carries a ↩ marker",
        rows.marks === rows.visualRows - 1,
        `${rows.marks} markers for ${rows.visualRows} rows`,
    );
    check(
        "row: no horizontal scrolling needed after wrapping",
        !rows.scrollable,
        rows.scrollable ? "preview-surface scrolls horizontally" : "fits",
    );
    await shot(page, "03-wrapped-row");

    // ---- 3. Selecting / hovering must not move anything -----------------
    const firstChip = track.locator(".seg-chip").first();
    const trackBefore = await box(track);
    const surfaceBefore = await box(page.locator(".preview-surface"));
    await firstChip.hover();
    await page.waitForTimeout(700); // let the tooltip appear
    const trackHover = await box(track);
    const surfaceHover = await box(page.locator(".preview-surface"));
    await firstChip.click();
    await page.waitForTimeout(500);
    const trackSel = await box(track);
    const surfaceSel = await box(page.locator(".preview-surface"));
    const same = (a: typeof trackBefore, b: typeof trackBefore) =>
        a.x === b.x && a.y === b.y && a.w === b.w && a.h === b.h;
    check(
        "stability: hovering a chip does not move/resize its row",
        same(trackBefore, trackHover),
        `${JSON.stringify(trackBefore)} vs ${JSON.stringify(trackHover)}`,
    );
    check(
        "stability: selecting a chip does not move/resize its row",
        same(trackBefore, trackSel),
        `${JSON.stringify(trackBefore)} vs ${JSON.stringify(trackSel)}`,
    );
    check(
        "stability: the preview surface keeps its size through hover+select",
        same(surfaceBefore, surfaceHover) && same(surfaceBefore, surfaceSel),
        `${JSON.stringify(surfaceBefore)} / ${JSON.stringify(surfaceHover)} / ${JSON.stringify(surfaceSel)}`,
    );
    await shot(page, "04-selected");

    // ---- 4. Settings modal hosts the language --------------------------
    const langInHeader = await page.locator('header button:has-text("日本語")').count();
    await page.locator('[data-testid="settings-button"]').click();
    await page.waitForSelector('[data-testid="setting-lang"]');
    const gitInModal = await page.locator('[data-testid="setting-git-cache-ttl"]').count();
    check(
        "settings: language lives in the modal, not the header toolbar",
        langInHeader === 0 && gitInModal === 1,
        `header toggles=${langInHeader}, git form in modal=${gitInModal}`,
    );
    await page.locator(".modal").screenshot({ path: `${OUT}/05-settings.png` });
    // Switch to Japanese and back, checking the UI actually re-renders.
    await page.selectOption('[data-testid="setting-lang"]', "ja");
    await page.waitForTimeout(400);
    await page.locator(".modal").screenshot({ path: `${OUT}/06-settings-ja.png` });
    const jaApplied = await page
        .locator(".modal")
        .textContent()
        .then((s) => (s ?? "").includes("言語"));
    check("settings: switching to Japanese re-renders the modal", jaApplied, "");
    await page.selectOption('[data-testid="setting-lang"]', "en");
    await page.waitForTimeout(300);
    await page.locator('[data-testid="modal-close"]').click();
    await page.waitForTimeout(300);

    // ---- 5. Variant condition builder ---------------------------------
    await page.locator("button", { hasText: "+ Add responsive" }).first().click();
    await page.waitForSelector(".variant-card", { timeout: 10000 });
    await page.waitForTimeout(600);
    const addBtn = page.locator('[data-testid^="variant-when-"][data-testid$="-add"]').first();
    const pickersBefore = await page
        .locator('[data-testid^="variant-when-"][data-testid$="-metric"]')
        .count();
    check(
        "variant: an unconditional variant shows only the add button",
        (await addBtn.count()) > 0 && pickersBefore === 0,
        `add=${await addBtn.count()}, pickers=${pickersBefore}`,
    );
    await shot(page, "07-variant-collapsed");

    const prefix = (await addBtn.getAttribute("data-testid"))!.replace(/-add$/, "");
    const cardBefore = await box(page.locator(".variant-card").first());
    await addBtn.click();
    await page.waitForSelector(`[data-testid="${prefix}-metric"]`);
    await shot(page, "08-variant-builder");

    // A metric with a closed value set gets a <select> and eq/ne only.
    await page.selectOption(`[data-testid="${prefix}-metric"]`, "account-type");
    await page.waitForTimeout(600);
    const valueTag = await page
        .locator(`[data-testid="${prefix}-value"]`)
        .evaluate((e) => e.tagName);
    const ops = await page
        .locator(`[data-testid="${prefix}-op"] option`)
        .evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value));
    check(
        "variant: a closed-set metric offers a value dropdown and eq/ne only",
        valueTag === "SELECT" && ops.join(",") === "eq,ne",
        `value=<${valueTag}>, ops=${ops.join(",")}`,
    );

    // A metric with no known values keeps the picked metric on screen even
    // though the condition is not yet complete (the reported bug).
    await page.selectOption(`[data-testid="${prefix}-metric"]`, "five-hour-percent");
    await page.waitForTimeout(600);
    const keptMetric = await page.locator(`[data-testid="${prefix}-metric"]`).inputValue();
    const valueTag2 = await page
        .locator(`[data-testid="${prefix}-value"]`)
        .evaluate((e) => e.tagName);
    check(
        "variant: a half-built condition keeps its metric selected",
        keptMetric === "five-hour-percent" && valueTag2 === "INPUT",
        `metric=${keptMetric}, value=<${valueTag2}>`,
    );
    await page.fill(`[data-testid="${prefix}-value"]`, "80");
    await page.waitForTimeout(800);
    const typed = await page.locator(`[data-testid="${prefix}-value"]`).inputValue();
    check("variant: the value box accepts typing", typed === "80", `value=${typed}`);
    await shot(page, "09-variant-condition");

    // The card must not jump around while the builder opens/fills.
    const cardAfter = await box(page.locator(".variant-card").first());
    check(
        "variant: opening the builder only grows the card downward (x/width stable)",
        cardBefore.x === cardAfter.x && cardBefore.w === cardAfter.w,
        `${JSON.stringify(cardBefore)} vs ${JSON.stringify(cardAfter)}`,
    );

    // Round-trip through the DSL editor: the condition must be in the source.
    const dslTab = page.locator('[data-testid="view-dsl"]');
    if ((await dslTab.count()) > 0) {
        await dslTab.click();
        await page.waitForTimeout(800);
        const src = (await page.locator("textarea").first().inputValue().catch(() => "")) || "";
        check(
            "variant: the built condition reaches the DSL source",
            src.includes("five-hour-percent ge 80") || src.includes("five-hour-percent eq 80"),
            src.match(/<variant[^>]*>/g)?.join(" ") ?? "no variant tag found",
        );
        await shot(page, "10-dsl-source");
    }

    check("console: no errors during the run", consoleErrors.length === 0, consoleErrors.join(" | "));

    writeFileSync(`${OUT}/results.json`, JSON.stringify(results, null, 2));
    await browser.close();

    const failed = results.filter((r) => !r.ok);
    console.log(`\n${results.length - failed.length}/${results.length} checks passed`);
    if (failed.length > 0) {
        process.exitCode = 1;
    }
}

await main();
