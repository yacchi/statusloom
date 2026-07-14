import { afterEach, describe, expect, it } from "vitest";
import {
    LANG_STORAGE_KEY,
    MESSAGES,
    detectLang,
    loadLang,
    pickDescription,
    saveLang,
    t,
} from "./i18n.ts";

afterEach(() => {
    window.localStorage.clear();
});

describe("detectLang", () => {
    it("maps ja* locales to ja", () => {
        expect(detectLang("ja")).toBe("ja");
        expect(detectLang("ja-JP")).toBe("ja");
        expect(detectLang("JA-JP")).toBe("ja");
    });

    it("maps everything else to en", () => {
        expect(detectLang("en-US")).toBe("en");
        expect(detectLang("fr")).toBe("en");
        expect(detectLang("")).toBe("en");
        expect(detectLang(undefined)).toBe("en");
        expect(detectLang(null)).toBe("en");
    });
});

describe("loadLang / saveLang", () => {
    it("prefers the persisted language over detection", () => {
        window.localStorage.setItem(LANG_STORAGE_KEY, "ja");
        expect(loadLang()).toBe("ja");
    });

    it("ignores invalid persisted values and falls back to detection", () => {
        window.localStorage.setItem(LANG_STORAGE_KEY, "fr");
        // jsdom's navigator.language is en-US
        expect(loadLang()).toBe("en");
    });

    it("saveLang persists to localStorage", () => {
        saveLang("ja");
        expect(window.localStorage.getItem(LANG_STORAGE_KEY)).toBe("ja");
        expect(loadLang()).toBe("ja");
    });
});

describe("messages", () => {
    it("ja covers every en key", () => {
        expect(Object.keys(MESSAGES.ja).sort()).toEqual(Object.keys(MESSAGES.en).sort());
    });

    it("t returns language-specific strings", () => {
        expect(t("en", "zeroDisabledHint")).toBe("0 = disabled");
        expect(t("ja", "zeroDisabledHint")).toBe("0 = 無効");
        expect(t("ja", "helpWhen")).toContain("context-percent ge 80");
        expect(t("en", "helpWhen")).toContain("context-percent ge 80");
    });
});

describe("pickDescription", () => {
    const entry = { descriptions: { en: "Model name", ja: "モデル名" } };

    it("picks the current language", () => {
        expect(pickDescription(entry, "ja")).toBe("モデル名");
        expect(pickDescription(entry, "en")).toBe("Model name");
    });

    it("falls back to en, then to empty", () => {
        expect(pickDescription({ descriptions: { en: "only en" } }, "ja")).toBe("only en");
        expect(pickDescription({ descriptions: {} }, "ja")).toBe("");
        expect(pickDescription({}, "ja")).toBe("");
        expect(pickDescription(undefined, "ja")).toBe("");
    });
});
