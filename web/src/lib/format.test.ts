import { describe, expect, it } from "vitest";
import {
  accountErrorLabel,
  balance,
  monthlyWindow,
  number,
  splitModels,
  usagePercent,
  isQuotaExhausted,
} from "./format";
import type { UsageReport } from "./types";
describe("quota presentation", () => {
  it("distinguishes depleted quotas from temporary upstream failures", () => {
    expect(
      isQuotaExhausted({
        enabled: true,
        status: "cooldown",
        lastError: "five_hour_exhausted",
      }),
    ).toBe(true);
    expect(
      isQuotaExhausted({
        enabled: true,
        status: "cooldown",
        lastError: "Upstream temporarily unavailable",
      }),
    ).toBe(false);
    expect(
      isQuotaExhausted({
        enabled: false,
        status: "disabled",
        lastError: "five_hour_exhausted",
      }),
    ).toBe(false);
    expect(accountErrorLabel("five_hour_exhausted")).toContain("5 小时");
    expect(accountErrorLabel("Unexpected upstream response")).toBe(
      "Unexpected upstream response",
    );
  });
  it("does not present unknown balances or caps as zero", () => {
    expect(number(null)).toBe("—");
    expect(
      usagePercent({ used: 0, cap: null, exceeded: false, resetAt: 0 }),
    ).toBeNull();
    expect(balance(null)).toBeNull();
    expect(monthlyWindow(null)).toBeNull();
  });
  it("clamps percentages while preserving known zero usage", () => {
    expect(
      usagePercent({ used: 150, cap: 100, exceeded: true, resetAt: 0 }),
    ).toBe(100);
    expect(
      usagePercent({ used: 0, cap: 100, exceeded: false, resetAt: 0 }),
    ).toBe(0);
  });
  it("requires every balance component before summing", () => {
    const report = {
      credits: { monthlyCredits: 10, purchasedCredits: 0, freeCredits: null },
    } as UsageReport;
    expect(balance(report)).toBeNull();
    report.credits!.freeCredits = 5;
    expect(balance(report)).toBe(15);
  });
  it("normalizes comma and newline model allowlists", () => {
    expect(splitModels(" a, b\na,,\n ")).toEqual(["a", "b"]);
  });
});
