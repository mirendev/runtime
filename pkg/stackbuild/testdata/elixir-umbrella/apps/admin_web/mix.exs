defmodule AdminWeb.MixProject do
  use Mix.Project

  def project do
    [app: :admin_web, version: "0.1.0", build_path: "../../_build",
     deps_path: "../../deps", lockfile: "../../mix.lock",
     aliases: ["assets.deploy": [&deploy_assets/1]]]
  end

  defp deploy_assets(_) do
    File.mkdir_p!("priv/static")
    File.write!("priv/static/app.js", "admin:" <> File.read!("assets/installed"))
  end
end
