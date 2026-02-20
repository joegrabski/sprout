import React, { ReactNode } from "react";
import Layout from "@theme/Layout";
import { HomepageHeader } from "../components/Landing/Header";
import { FeaturesSection } from "../components/Landing/Features";
import { DemoSection } from "../components/Landing/Demo";
import { TUISection } from "../components/Landing/TUI";
import { TmuxSection } from "../components/Landing/Tmux";
import { CTASection } from "../components/Landing/CTA";

export default function Home(): ReactNode {
  return (
    <Layout
      title="Branch fearlessly. Stay in flow."
      description="Sprout manages your git worktrees with isolated tmux sessions and AI agents, so you can work across multiple branches without losing your place."
    >
      <HomepageHeader />
      <main>
        <FeaturesSection />
        <DemoSection />
        <TUISection />
        <TmuxSection />
        <CTASection />
      </main>
    </Layout>
  );
}
