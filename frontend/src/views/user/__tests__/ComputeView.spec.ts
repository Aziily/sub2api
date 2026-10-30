import { describe, expect, it, vi, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ComputeView from "../ComputeView.vue";
vi.mock("vue-i18n", () => ({useI18n: () => ({t: (key: string) => key})}));
const mocks = vi.hoisted(() => ({ path: "/compute", get: vi.fn() }));
vi.mock("vue-router", () => ({ useRoute: () => ({ path: mocks.path }) }));
vi.mock("@/api/client", () => ({ default: { get: mocks.get } }));
vi.mock("@/components/layout/AppLayout.vue", () => ({
  default: { template: "<div><slot /></div>" },
}));
afterEach(() => { vi.clearAllMocks(); mocks.path="/compute"; });
describe("Compute resource view", () => {
  it("shows only eligible hardware and user credits without upstream account information", async () => {
    mocks.get.mockImplementation((path: string) =>
      Promise.resolve({
        data: path.endsWith("/jobs")
          ? []
          : {
              ready: true,
              platform_credits: 12.5,
              usage_summary: { total_jobs: 2, succeeded_jobs: 1 },
              hardware_options: [
                {
                  id: "A100",
                  label: "A100 GPU",
                  eligible: true,
                  vram_gb: 40,
                  memory_source: "observed",
                },
                { id: "H100", label: "H100 GPU", eligible: false },
              ],
              accounts: [
                { id: 1, email: "private@example.com", enabled: true },
              ],
            },
      }),
    );
    const wrapper = mount(ComputeView, {
      global: {
        stubs: { RouterLink: { template: "<a><slot /></a>" } },
      },
    });
    await flushPromises();
    expect(wrapper.text()).toContain("40 GB");
    expect(wrapper.text()).not.toContain("H100");
    expect(wrapper.text()).not.toContain("private@example.com");
    expect(wrapper.get('[data-testid="compute-credits"]').text()).toBe("12.5");
    expect(wrapper.get("details").attributes("open")).toBeUndefined();
    expect(wrapper.get("a[download]").attributes("href")).toBe(
      "/compute/v1/cli",
    );
    wrapper.unmount();
  });
  it("shows Google CU rather than the administrator wallet and labels stale snapshots", async () => {
    mocks.path="/admin/compute";
    mocks.get.mockImplementation((path:string)=>Promise.resolve({data:path.endsWith("/jobs")?[]:{platform_credits:999,compute_units:199.9,accounts:[{id:1,email:"owner@example.com",enabled:true,consumption:{currentBalance:199.9,consumptionRateHourly:0,assignmentsCount:0},consumption_error:"CU unavailable"}]}}));
    const w=mount(ComputeView,{global:{stubs:{RouterLink:{template:"<a><slot /></a>"}}}});
    await flushPromises();
    expect(w.get('[data-testid="compute-credits"]').text()).toBe("199.9");
    expect(w.text()).toContain("compute.cuStale");
    expect(w.text()).not.toContain("999");w.unmount();
  });

  it("selects priced hardware and disables eligible but unpriced hardware", async () => {
    mocks.get.mockImplementation((path:string)=>Promise.resolve({data:path.endsWith("/jobs")?[]:{ready:true,compute_billing_enabled:true,pricing:{enabled:true,version:4,rates:{A100:"0.81602"}},hardware_options:[{id:"CPU",label:"CPU",eligible:true},{id:"A100",label:"A100",eligible:true}]}}));
    const w=mount(ComputeView,{global:{stubs:{RouterLink:{template:"<a><slot /></a>"}}}});
    await flushPromises();
    expect((w.get('select').element as HTMLSelectElement).value).toBe('A100');
    expect(w.get('option[value="CPU"]').attributes('disabled')).toBeDefined();
    expect(w.text()).toContain('compute.priceMissing');w.unmount();
  });

});
