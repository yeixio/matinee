# Homebrew formula written by the release workflow from
# packaging/homebrew/matinee.rb.tmpl. Edit the template, not Formula/matinee.rb.
class Matinee < Formula
  desc "Self-hosted media server for movies, TV and music"
  homepage "https://matinee.yeix.io"
  version "0.1.6"
  license "AGPL-3.0-or-later"

  depends_on "ffmpeg"

  on_macos do
    on_arm do
      url "https://github.com/yeixio/matinee/releases/download/v0.1.6/matinee_0.1.6_darwin_arm64.tar.gz"
      sha256 "175fa566f0cd7f0bdd422988036613b0e64367548cca43dd4208af47defbbbac"
    end
    on_intel do
      url "https://github.com/yeixio/matinee/releases/download/v0.1.6/matinee_0.1.6_darwin_amd64.tar.gz"
      sha256 "b7c5fa35daf5e01726a0133bc5a492c2b175203857b2be42478476d37a2bb5fe"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/yeixio/matinee/releases/download/v0.1.6/matinee_0.1.6_linux_arm64.tar.gz"
      sha256 "7299ec311f6cff46b4de5dd03f7cdaa9e8f2673dee32ffc441c0f37711837134"
    end
    on_intel do
      url "https://github.com/yeixio/matinee/releases/download/v0.1.6/matinee_0.1.6_linux_amd64.tar.gz"
      sha256 "dbcb74d1ef70e6bbe3520a19c72d5b18620ab8bd79fff5dc5038d98c324ce701"
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
