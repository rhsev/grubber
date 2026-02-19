require "yaml"
require "json"
require "option_parser"

module DataGrubber
  VERSION       = "0.4.1"
  CONFIG_PATH   = Path.home.join(".config/grubber/config.yaml").to_s

  FRONTMATTER_REGEX = /\A---\n(.*?)\n---\n/m
  YAML_BLOCK_REGEX  = /```yaml\n(.*?)\n```/m
  FILTER_REGEX      = /^([^=~^!]+)([=~^!])(.*)$/

  alias YAMLValue = YAML::Any
  alias Record = Hash(String, YAMLValue)

  # Configuration loader
  class Config
    getter defaults : Hash(String, YAML::Any)
    getter sets : Hash(String, YAML::Any)

    def initialize
      @defaults = {
        "blocks_only"  => YAML::Any.new(false),
        "array_fields" => YAML::Any.new([] of YAML::Any),
        "filters"      => YAML::Any.new([] of YAML::Any),
      }
      @sets = {} of String => YAML::Any
      load_config if File.exists?(CONFIG_PATH)
    end

    def load_config
      content = File.read(CONFIG_PATH)
      config = YAML.parse(content)

      if defaults_yaml = config["defaults"]?
        defaults_yaml.as_h.each do |k, v|
          @defaults[k.as_s] = v
        end
      end

      if sets_yaml = config["sets"]?
        sets_yaml.as_h.each do |k, v|
          @sets[k.as_s] = v
        end
      end
    rescue ex
      STDERR.puts "Warning: Could not load config: #{ex.message}"
    end

    def get_set(name : String) : Hash(String, YAML::Any)?
      if set = @sets[name]?
        result = {} of String => YAML::Any
        set.as_h.each { |k, v| result[k.as_s] = v }
        result
      else
        nil
      end
    end

    def set_names : Array(String)
      @sets.keys
    end

    def default_blocks_only : Bool
      @defaults["blocks_only"]?.try(&.as_bool?) || false
    end

    def default_array_fields : Array(String)
      @defaults["array_fields"]?.try(&.as_a.map(&.as_s)) || [] of String
    end

    def default_filters : Array(String)
      @defaults["filters"]?.try(&.as_a.map(&.as_s)) || [] of String
    end
  end

  # Filter condition
  struct Condition
    property field : String
    property op : Char
    property value : String

    def initialize(@field, @op, @value)
    end
  end

  # Filter for matching records
  class Filter
    @conditions : Array(Condition)

    def initialize(filters : Array(String))
      @conditions = filters.map { |f| parse_filter(f) }
    end

    def match?(record : Record) : Bool
      @conditions.all? { |cond| matches_condition?(record, cond) }
    end

    private def parse_filter(filter_str : String) : Condition
      if match = filter_str.match(FILTER_REGEX)
        Condition.new(match[1].strip, match[2][0], match[3].strip.downcase)
      else
        raise "Invalid filter syntax: #{filter_str}"
      end
    end

    private def matches_condition?(record : Record, cond : Condition) : Bool
      field_value = record[cond.field]?
      return cond.op == '!' if field_value.nil?

      values = get_values(field_value)
      values_downcase = values.map(&.downcase)

      case cond.op
      when '='
        values_downcase.any? { |v| v == cond.value }
      when '~'
        values_downcase.any? { |v| v.includes?(cond.value) }
      when '^'
        values_downcase.any? { |v| v.starts_with?(cond.value) }
      when '!'
        values_downcase.none? { |v| v == cond.value }
      else
        false
      end
    end

    private def get_values(value : YAMLValue) : Array(String)
      if arr = value.as_a?
        arr.map { |v| v.as_s? || v.to_s }.compact
      else
        [value.as_s? || value.to_s]
      end
    end
  end

  class Grubber
    getter notes_dir : String
    getter blocks_only : Bool
    getter frontmatter_only : Bool
    getter array_fields : Array(String)
    @filter : Filter?

    def initialize(@notes_dir : String, @blocks_only : Bool = false,
                   @frontmatter_only : Bool = false,
                   @array_fields : Array(String) = [] of String,
                   filters : Array(String) = [] of String)
      @notes_dir = File.expand_path(@notes_dir)
      @filter = filters.empty? ? nil : Filter.new(filters)
      raise "Directory not found: #{@notes_dir}" unless Dir.exists?(@notes_dir)
    end

    def extract(files : Array(String)? = nil) : {records: Array(Record), keys: Array(String)}
      records = [] of Record
      all_keys = Set(String).new
      files_to_process = files || markdown_files

      files_to_process.each do |file|
        note = parse_note(file)
        note[:records].each do |record|
          flat_record = note[:metadata].merge(record)
          flat_record = normalize_arrays(flat_record) unless @array_fields.empty?
          if filter = @filter
            next unless filter.match?(flat_record)
          end
          records << flat_record
          flat_record.keys.each { |k| all_keys << k }
        end
      end

      {records: records, keys: all_keys.to_a.sort}
    end

    def output_json(records : Array(Record), output : IO = STDOUT)
      output.puts records.to_pretty_json
    end

    def output_tsv(records : Array(Record), keys : Array(String), output : IO = STDOUT)
      return if records.empty?

      # Header
      output.puts keys.join("\t")

      # Rows
      records.each do |record|
        row = keys.map do |key|
          value = record[key]?
          if value.nil?
            ""
          elsif arr = value.as_a?
            arr.map { |v| v.as_s? || v.to_s }.join(", ")
          else
            (value.as_s? || value.to_s).gsub(/[\t\n\r]/, ' ')
          end
        end
        output.puts row.join("\t")
      end
    end

    private def markdown_files : Array(String)
      Dir.glob("#{@notes_dir}/**/*.md")
    end

    private def parse_note(file_path : String)
      content = File.read(file_path)

      frontmatter_match = content.match(FRONTMATTER_REGEX)

      if frontmatter_match
        frontmatter = parse_yaml_string(frontmatter_match[1])
        body = frontmatter_match.post_match
      else
        frontmatter = Record.new
        body = content
      end

      # Frontmatter-only mode: skip YAML block parsing
      if @frontmatter_only
        return build_result(file_path, frontmatter, [] of Record)
      end

      unless body.includes?("```yaml")
        return build_result(file_path, frontmatter, [] of Record)
      end

      yaml_records = parse_yaml_blocks(body)
      build_result(file_path, frontmatter, yaml_records)
    end

    private def build_result(file_path : String, frontmatter : Record, records : Array(Record))
      metadata = frontmatter.dup
      metadata["_note_file"] = YAML::Any.new(file_path)

      if records.empty? && !@blocks_only && !@frontmatter_only
        records = [Record.new]
      end

      if records.empty? && @frontmatter_only
        records = [Record.new]
      end

      {metadata: metadata, records: records}
    end

    private def parse_yaml_string(yaml_content : String) : Record
      parsed = YAML.parse(yaml_content)
      return Record.new unless parsed.as_h?

      result = Record.new
      parsed.as_h.each do |k, v|
        result[k.as_s] = stringify_dates(v)
      end
      result
    rescue ex : YAML::ParseException
      STDERR.puts "Warning: Could not parse YAML: #{ex.message}"
      Record.new
    end

    private def parse_yaml_blocks(body : String) : Array(Record)
      records = [] of Record

      body.scan(YAML_BLOCK_REGEX) do |match|
        record = parse_yaml_string(match[1])
        records << record unless record.empty?
      end

      records
    end

    private def stringify_dates(value : YAML::Any) : YAML::Any
      raw = value.raw

      case raw
      when Time
        YAML::Any.new(raw.to_s("%Y-%m-%d"))
      when Hash
        new_hash = {} of YAML::Any => YAML::Any
        raw.each do |k, v|
          new_hash[k] = stringify_dates(v)
        end
        YAML::Any.new(new_hash)
      when Array
        YAML::Any.new(raw.map { |v| stringify_dates(v) })
      else
        value
      end
    end

    private def normalize_arrays(record : Record) : Record
      result = Record.new

      record.each do |key, value|
        if @array_fields.includes?(key) && value.as_s?
          result[key] = YAML::Any.new([value])
        else
          result[key] = value
        end
      end

      result
    end
  end
end

# CLI Interface with tri-state logic
class GrubberCLI
  property output_file : String? = nil
  property format : String? = nil
  property blocks_only : Bool? = nil
  property frontmatter_only : Bool? = nil
  property array_fields : Array(String)? = nil
  property filters : Array(String) = [] of String
  property set_name : String? = nil
  property notes_dir : String? = nil

  def run(args : Array(String))
    if args.empty? || args.first.in?("-h", "--help")
      print_help
      exit 0
    end

    if args.first.in?("-v", "--version")
      puts "grubber #{DataGrubber::VERSION} (Crystal)"
      exit 0
    end

    command = args.shift

    case command
    when "extract"
      run_extract(args)
    else
      if Dir.exists?(command)
        @notes_dir = command
        run_extract(args)
      else
        STDERR.puts "Unknown command: #{command}"
        exit 1
      end
    end
  end

  private def run_extract(args : Array(String))
    OptionParser.parse(args) do |parser|
      parser.on("-o FILE", "--output=FILE", "Write output to file") do |file|
        @output_file = file
      end
      parser.on("-s NAME", "--set=NAME", "Load options from config set") do |name|
        @set_name = name
      end
      parser.on("--format=FORMAT", "Output format: json (default) or tsv") do |fmt|
        @format = fmt
      end
      parser.on("-b", "--blocks-only", "Only extract YAML blocks") do
        @blocks_only = true
      end
      parser.on("-m", "--frontmatter-only", "Only extract frontmatter, ignore YAML blocks") do
        @frontmatter_only = true
      end
      parser.on("--array-fields=FIELDS", "Normalize fields to arrays (comma-separated)") do |fields|
        @array_fields = fields.split(",").map(&.strip)
      end
      parser.on("-f EXPR", "--filter=EXPR", "Filter records (can be used multiple times)") do |expr|
        @filters << expr
      end
      parser.on("-h", "--help", "Show help") do
        print_extract_help
        exit 0
      end
      parser.unknown_args do |remaining|
        if remaining.size > 0 && Dir.exists?(remaining[0])
          @notes_dir = remaining[0]
        end
      end
    end

    execute
  end

  private def execute
    config = DataGrubber::Config.new
    set_config = @set_name ? config.get_set(@set_name.not_nil!) : nil

    if @set_name && set_config.nil?
      STDERR.puts "Error: Unknown set '#{@set_name}'"
      STDERR.puts "Available sets: #{config.set_names.join(", ")}" if config.set_names.any?
      exit 1
    end

    set_config ||= {} of String => YAML::Any

    # Build final options with hierarchy: CLI > Set > Env > Config Defaults > Hardcoded
    final_format = @format || "json"

    final_notes_dir = @notes_dir ||
                      set_config["path"]?.try(&.as_s).try { |p| File.expand_path(p) } ||
                      ENV["GRUBBER_NOTES"]? ||
                      Dir.current

    final_blocks_only = @blocks_only
    if final_blocks_only.nil?
      final_blocks_only = set_config["blocks_only"]?.try(&.as_bool?)
    end
    if final_blocks_only.nil?
      final_blocks_only = config.default_blocks_only
    end

    final_frontmatter_only = @frontmatter_only
    if final_frontmatter_only.nil?
      final_frontmatter_only = set_config["frontmatter_only"]?.try(&.as_bool?)
    end
    final_frontmatter_only ||= false

    final_array_fields = @array_fields ||
                         set_config["array_fields"]?.try(&.as_a.map(&.as_s)) ||
                         ENV["GRUBBER_ARRAY_FIELDS"]?.try(&.split(",").map(&.strip)) ||
                         config.default_array_fields

    # Filters: merge from defaults + set + CLI
    set_filters = set_config["filters"]?.try(&.as_a.map(&.as_s)) || [] of String
    final_filters = (config.default_filters + set_filters + @filters).uniq

    grubber = DataGrubber::Grubber.new(final_notes_dir, final_blocks_only, final_frontmatter_only, final_array_fields, final_filters)
    result = grubber.extract

    if result[:records].empty?
      STDERR.puts "No YAML records found in #{final_notes_dir}"
      exit 0
    end

    output_target = @output_file ? File.open(@output_file.not_nil!, "w") : STDOUT

    begin
      case final_format
      when "tsv"
        grubber.output_tsv(result[:records], result[:keys], output_target)
      else
        grubber.output_json(result[:records], output_target)
      end
    ensure
      output_target.close if @output_file
    end

    if @output_file
      STDERR.puts "Extracted #{result[:records].size} records to #{@output_file}"
    end
  end

  private def print_help
    puts <<-HELP
    grubber v#{DataGrubber::VERSION} (Crystal) - Extract structured data from Markdown notes

    USAGE:
      grubber [command] [options]

    COMMANDS:
      extract      Extract YAML blocks from Markdown files
      version      Show version
      help         Show this help

    EXAMPLES:
      grubber extract ~/notes -o data.json
      grubber extract --set vertrag --format tsv
      grubber extract -f "type=vertrag" -f "due^2025-02"

    CONFIG:
      #{DataGrubber::CONFIG_PATH}

    ENVIRONMENT:
      GRUBBER_NOTES         Default notes directory
      GRUBBER_ARRAY_FIELDS  Default fields to normalize (comma-separated)
    HELP
  end

  private def print_extract_help
    puts <<-HELP
    Usage: grubber extract [directory] [options]

    Options:
      -o, --output=FILE       Write output to file instead of stdout
      -s, --set=NAME          Load options from config set
      --format=FORMAT         Output format: json (default) or tsv
      -b, --blocks-only       Only extract YAML blocks, ignore frontmatter-only notes
      -m, --frontmatter-only  Only extract frontmatter, ignore YAML blocks
      --array-fields=FIELDS   Normalize fields to arrays (comma-separated)
      -f, --filter=EXPR       Filter records (can be used multiple times)
                              Operators: = (equals), ~ (contains), ^ (starts with), ! (not equals)
                              Examples: type=vertrag, due^2025-02, name~versicher
      -h, --help              Show this help
    HELP
  end
end

GrubberCLI.new.run(ARGV.dup)
