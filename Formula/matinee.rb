# Homebrew formula written by the release workflow from
# packaging/homebrew/matinee.rb.tmpl. Edit the template, not Formula/matinee.rb.
class Matinee < Formula
  desc "Self-hosted media server for movies, TV and music"
  homepage "https://matinee.yeix.io"
  version "0.2.10"
  license "AGPL-3.0-or-later"

  depends_on "ffmpeg"

  on_macos do
    on_arm do
      url "https://github.com/yeixio/matinee/releases/download/v0.2.10/matinee_0.2.10_darwin_arm64.tar.gz"
      sha256 "a5ab77cc66c1045f98c8885ecf9e1de83705c4b0439795f21785bd9eb164656b"
    end
    on_intel do
      url "https://github.com/yeixio/matinee/releases/download/v0.2.10/matinee_0.2.10_darwin_amd64.tar.gz"
      sha256 "7759e3f75980eb994932229a6f7c1675b62c341c991e1da688ebc91643843584"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/yeixio/matinee/releases/download/v0.2.10/matinee_0.2.10_linux_arm64.tar.gz"
      sha256 "3c30a7329b3430d7301792e7fb117c891ff9e50fdade49a5a8f05895c6f3e232"
    end
    on_intel do
      url "https://github.com/yeixio/matinee/releases/download/v0.2.10/matinee_0.2.10_linux_amd64.tar.gz"
      sha256 "f22fee6b024d4d9b5cbaaabcc61f86d5072ea4b33a97388bb819fccfe6b0b886"
    end
  end

  def install
    bin.install "matinee"
  end

  def post_install
    (var/"matinee").mkpath
    env = etc/"matinee/matinee.env"
    return if env.exist?

    env.dirname.mkpath
    env.write <<~EOS
      # Matinee settings, read by `brew services`. After a change:
      #   brew services restart matinee
      # Every setting is listed at https://github.com/yeixio/matinee#configuration

      # TMDB key for posters and descriptions. How to get one:
      # https://github.com/yeixio/matinee#posters-and-descriptions-tmdb-key
      #MATINEE_TMDB_KEY=

      # Listen address.
      #MATINEE_ADDR=:7171
    EOS
  end

  service do
    # Reads the settings file, so changing one doesn't mean editing the service.
    env = etc/"matinee/matinee.env"
    run ["/bin/sh", "-c", "set -a; [ -f '#{env}' ] && . '#{env}'; exec '#{opt_bin}/matinee'"]
    environment_variables MATINEE_DATA_DIR: var/"matinee", MATINEE_MEDIA_ROOT: Dir.home
    keep_alive true
    log_path var/"log/matinee.log"
    error_log_path var/"log/matinee.log"
  end

  def caveats
    <<~EOS
      Start Matinee now and at login:
        brew services start matinee

      Then open http://localhost:7171 to create the admin account and add
      your folders. Settings, such as a TMDB key for posters, go in
        #{etc}/matinee/matinee.env
      and data lives in #{var}/matinee.
    EOS
  end

  test do
    port = free_port
    pid = spawn({ "MATINEE_DATA_DIR" => testpath.to_s, "MATINEE_ADDR" => "127.0.0.1:#{port}" }, bin/"matinee")
    sleep 3
    assert_match "ok", shell_output("curl -fsS http://127.0.0.1:#{port}/healthz")
  ensure
    Process.kill("TERM", pid)
  end
end
