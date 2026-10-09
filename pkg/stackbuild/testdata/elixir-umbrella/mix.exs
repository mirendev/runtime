defmodule Shop.Umbrella.MixProject do
  use Mix.Project

  def project do
    [apps_path: "apps", version: "0.1.0",
     releases: [shop: [applications: [shop_web: :permanent, admin_web: :permanent]]]]
  end
end
