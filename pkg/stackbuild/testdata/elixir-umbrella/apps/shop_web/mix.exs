defmodule ShopWeb.MixProject do
  use Mix.Project

  def project do
    [app: :shop_web, version: "0.1.0", build_path: "../../_build",
     deps_path: "../../deps", lockfile: "../../mix.lock",
     aliases: ["assets.deploy": [&deploy_assets/1]]]
  end

  defp deploy_assets(_) do
    if File.exists?("assets/node_modules/stale"), do: raise("copied laptop node_modules")
    File.mkdir_p!("priv/static")
    File.write!("priv/static/app.js", "shop:" <> File.read!("assets/installed"))
  end
end
